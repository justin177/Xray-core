package inbound

import (
	"bytes"
	"context"
	"io"
	"reflect"
	"time"
	"unsafe"

	gotls "crypto/tls"

	"github.com/xtls/xray-core/common"
	"github.com/xtls/xray-core/common/buf"
	"github.com/xtls/xray-core/common/errors"
	"github.com/xtls/xray-core/common/log"
	"github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/common/protocol"
	"github.com/xtls/xray-core/common/session"
	"github.com/xtls/xray-core/common/signal"
	"github.com/xtls/xray-core/common/task"
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/features/dns"
	"github.com/xtls/xray-core/features/policy"
	"github.com/xtls/xray-core/features/routing"
	"github.com/xtls/xray-core/proxy"
	"github.com/xtls/xray-core/proxy/fallback"
	"github.com/xtls/xray-core/proxy/vless"
	"github.com/xtls/xray-core/proxy/vless/encoding"
	"github.com/xtls/xray-core/transport/internet/reality"
	"github.com/xtls/xray-core/transport/internet/stat"
	"github.com/xtls/xray-core/transport/internet/tls"

	feature_inbound "github.com/xtls/xray-core/features/inbound"
)

func init() {
	common.Must(common.RegisterConfig((*Config)(nil), func(ctx context.Context, config interface{}) (interface{}, error) {
		var dc dns.Client
		if err := core.RequireFeatures(ctx, func(d dns.Client) error {
			dc = d
			return nil
		}); err != nil {
			return nil, err
		}

		c := config.(*Config)

		validator := new(vless.MemoryValidator)
		for _, user := range c.Clients {
			u, err := user.ToMemoryUser()
			if err != nil {
				return nil, errors.New("failed to get VLESS user").Base(err).AtError()
			}
			if err := validator.Add(u); err != nil {
				return nil, errors.New("failed to initiate user").Base(err).AtError()
			}
		}

		return New(ctx, c, dc, validator)
	}))
}

// Handler is an inbound connection handler that handles messages in VLess protocol.
type Handler struct {
	inboundHandlerManager feature_inbound.Manager
	policyManager         policy.Manager
	validator             vless.Validator
	dns                   dns.Client
	// regexps               map[string]*regexp.Regexp       // or nil

	fallback *fallback.Handler
}

// New creates a new VLess inbound handler.
func New(ctx context.Context, config *Config, dc dns.Client, validator vless.Validator) (*Handler, error) {
	v := core.MustFromContext(ctx)
	handler := &Handler{
		inboundHandlerManager: v.GetFeature(feature_inbound.ManagerType()).(feature_inbound.Manager),
		policyManager:         v.GetFeature(policy.ManagerType()).(policy.Manager),
		dns:                   dc,
		validator:             validator,
	}

	if config.Fallbacks != nil {
		handler.fallback = fallback.New(&fallback.Config{Fallbacks: config.Fallbacks})
	}

	return handler, nil
}

func isMuxAndNotXUDP(request *protocol.RequestHeader, first *buf.Buffer) bool {
	if request.Command != protocol.RequestCommandMux {
		return false
	}
	if first.Len() < 7 {
		return true
	}
	firstBytes := first.Bytes()
	return !(firstBytes[2] == 0 && // ID high
		firstBytes[3] == 0 && // ID low
		firstBytes[6] == 2) // Network type: UDP
}

// Close implements common.Closable.Close().
func (h *Handler) Close() error {
	return errors.Combine(common.Close(h.validator))
}

// AddUser implements proxy.UserManager.AddUser().
func (h *Handler) AddUser(ctx context.Context, u *protocol.MemoryUser) error {
	return h.validator.Add(u)
}

// RemoveUser implements proxy.UserManager.RemoveUser().
func (h *Handler) RemoveUser(ctx context.Context, e string) error {
	return h.validator.Del(e)
}

// GetUser implements proxy.UserManager.GetUser().
func (h *Handler) GetUser(ctx context.Context, email string) *protocol.MemoryUser {
	return h.validator.GetByEmail(email)
}

// GetUsers implements proxy.UserManager.GetUsers().
func (h *Handler) GetUsers(ctx context.Context) []*protocol.MemoryUser {
	return h.validator.GetAll()
}

// GetUsersCount implements proxy.UserManager.GetUsersCount().
func (h *Handler) GetUsersCount(context.Context) int64 {
	return h.validator.GetCount()
}

// Network implements proxy.Inbound.Network().
func (*Handler) Network() []net.Network {
	return []net.Network{net.Network_TCP, net.Network_UNIX}
}

// Process implements proxy.Inbound.Process().
func (h *Handler) Process(ctx context.Context, network net.Network, connection stat.Connection, dispatcher routing.Dispatcher) error {
	iConn := connection
	if statConn, ok := iConn.(*stat.CounterConnection); ok {
		iConn = statConn.Connection
	}

	sessionPolicy := h.policyManager.ForLevel(0)
	if err := connection.SetReadDeadline(time.Now().Add(sessionPolicy.Timeouts.Handshake)); err != nil {
		return errors.New("unable to set read deadline").Base(err).AtWarning()
	}

	first := buf.FromBytes(make([]byte, buf.Size))
	first.Clear()
	firstLen, errR := first.ReadFrom(connection)
	if errR != nil {
		return errR
	}
	errors.LogInfo(ctx, "firstLen = ", firstLen)

	reader := &buf.BufferedReader{
		Reader: buf.NewReader(connection),
		Buffer: buf.MultiBuffer{first},
	}

	var request *protocol.RequestHeader
	var requestAddons *encoding.Addons
	var err error

	isfb := h.fallback != nil

	if isfb && firstLen < 18 {
		err = errors.New("fallback directly")
	} else {
		request, requestAddons, isfb, err = encoding.DecodeRequestHeader(isfb, first, reader, h.validator)
	}

	if err != nil {
		if isfb {
			return h.fallback.Process(ctx, err, sessionPolicy, connection, iConn, first, firstLen, reader)
		}

		if errors.Cause(err) != io.EOF {
			log.Record(&log.AccessMessage{
				From:   connection.RemoteAddr(),
				To:     "",
				Status: log.AccessRejected,
				Reason: err,
			})
			err = errors.New("invalid request from ", connection.RemoteAddr()).Base(err).AtInfo()
		}
		return err
	}

	if err := connection.SetReadDeadline(time.Time{}); err != nil {
		errors.LogWarningInner(ctx, err, "unable to set back read deadline")
	}
	errors.LogInfo(ctx, "received request for ", request.Destination())

	inbound := session.InboundFromContext(ctx)
	if inbound == nil {
		panic("no inbound metadata")
	}
	inbound.Name = "vless"
	inbound.User = request.User

	account := request.User.Account.(*vless.MemoryAccount)

	responseAddons := &encoding.Addons{
		// Flow: requestAddons.Flow,
	}

	var input *bytes.Reader
	var rawInput *bytes.Buffer
	switch requestAddons.Flow {
	case vless.XRV:
		if account.Flow == requestAddons.Flow {
			inbound.CanSpliceCopy = 2
			switch request.Command {
			case protocol.RequestCommandUDP:
				return errors.New(requestAddons.Flow + " doesn't support UDP").AtWarning()
			case protocol.RequestCommandMux:
				fallthrough // we will break Mux connections that contain TCP requests
			case protocol.RequestCommandTCP:
				var t reflect.Type
				var p uintptr
				if tlsConn, ok := iConn.(*tls.Conn); ok {
					if tlsConn.ConnectionState().Version != gotls.VersionTLS13 {
						return errors.New(`failed to use `+requestAddons.Flow+`, found outer tls version `, tlsConn.ConnectionState().Version).AtWarning()
					}
					t = reflect.TypeOf(tlsConn.Conn).Elem()
					p = uintptr(unsafe.Pointer(tlsConn.Conn))
				} else if realityConn, ok := iConn.(*reality.Conn); ok {
					t = reflect.TypeOf(realityConn.Conn).Elem()
					p = uintptr(unsafe.Pointer(realityConn.Conn))
				} else {
					return errors.New("XTLS only supports TLS and REALITY directly for now.").AtWarning()
				}
				i, _ := t.FieldByName("input")
				r, _ := t.FieldByName("rawInput")
				input = (*bytes.Reader)(unsafe.Pointer(p + i.Offset))
				rawInput = (*bytes.Buffer)(unsafe.Pointer(p + r.Offset))
			}
		} else {
			return errors.New("account " + account.ID.String() + " is not able to use the flow " + requestAddons.Flow).AtWarning()
		}
	case "":
		inbound.CanSpliceCopy = 3
		if account.Flow == vless.XRV && (request.Command == protocol.RequestCommandTCP || isMuxAndNotXUDP(request, first)) {
			return errors.New("account " + account.ID.String() + " is rejected since the client flow is empty. Note that the pure TLS proxy has certain TLS in TLS characters.").AtWarning()
		}
	default:
		return errors.New("unknown request flow " + requestAddons.Flow).AtWarning()
	}

	if request.Command != protocol.RequestCommandMux {
		ctx = log.ContextWithAccessMessage(ctx, &log.AccessMessage{
			From:   connection.RemoteAddr(),
			To:     request.Destination(),
			Status: log.AccessAccepted,
			Reason: "",
			Email:  request.User.Email,
		})
	} else if account.Flow == vless.XRV {
		ctx = session.ContextWithAllowedNetwork(ctx, net.Network_UDP)
	}

	sessionPolicy = h.policyManager.ForLevel(request.User.Level)
	ctx, cancel := context.WithCancel(ctx)
	timer := signal.CancelAfterInactivity(ctx, cancel, sessionPolicy.Timeouts.ConnectionIdle)
	inbound.Timer = timer
	ctx = policy.ContextWithBufferPolicy(ctx, sessionPolicy.Buffer)

	link, err := dispatcher.Dispatch(ctx, request.Destination())
	if err != nil {
		return errors.New("failed to dispatch request to ", request.Destination()).Base(err).AtWarning()
	}

	serverReader := link.Reader // .(*pipe.Reader)
	serverWriter := link.Writer // .(*pipe.Writer)
	trafficState := proxy.NewTrafficState(account.ID.Bytes())
	postRequest := func() error {
		defer timer.SetTimeout(sessionPolicy.Timeouts.DownlinkOnly)

		// default: clientReader := reader
		clientReader := encoding.DecodeBodyAddons(reader, request, requestAddons)

		var err error

		if requestAddons.Flow == vless.XRV {
			ctx1 := session.ContextWithInbound(ctx, nil) // TODO enable splice
			clientReader = proxy.NewVisionReader(clientReader, trafficState, true, ctx1)
			err = encoding.XtlsRead(clientReader, serverWriter, timer, connection, input, rawInput, trafficState, nil, true, ctx1)
		} else {
			// from clientReader.ReadMultiBuffer to serverWriter.WriteMultiBuffer
			err = buf.Copy(clientReader, serverWriter, buf.UpdateActivity(timer))
		}

		if err != nil {
			return errors.New("failed to transfer request payload").Base(err).AtInfo()
		}

		return nil
	}

	getResponse := func() error {
		defer timer.SetTimeout(sessionPolicy.Timeouts.UplinkOnly)

		bufferWriter := buf.NewBufferedWriter(buf.NewWriter(connection))
		if err := encoding.EncodeResponseHeader(bufferWriter, request, responseAddons); err != nil {
			return errors.New("failed to encode response header").Base(err).AtWarning()
		}

		// default: clientWriter := bufferWriter
		clientWriter := encoding.EncodeBodyAddons(bufferWriter, request, requestAddons, trafficState, false, ctx)
		multiBuffer, err1 := serverReader.ReadMultiBuffer()
		if err1 != nil {
			return err1 // ...
		}
		if err := clientWriter.WriteMultiBuffer(multiBuffer); err != nil {
			return err // ...
		}
		// Flush; bufferWriter.WriteMultiBuffer now is bufferWriter.writer.WriteMultiBuffer
		if err := bufferWriter.SetBuffered(false); err != nil {
			return errors.New("failed to write A response payload").Base(err).AtWarning()
		}

		var err error
		if requestAddons.Flow == vless.XRV {
			err = encoding.XtlsWrite(serverReader, clientWriter, timer, connection, trafficState, nil, false, ctx)
		} else {
			// from serverReader.ReadMultiBuffer to clientWriter.WriteMultiBuffer
			err = buf.Copy(serverReader, clientWriter, buf.UpdateActivity(timer))
		}
		if err != nil {
			return errors.New("failed to transfer response payload").Base(err).AtInfo()
		}
		// Indicates the end of response payload.
		switch responseAddons.Flow {
		default:
		}

		return nil
	}

	if err := task.Run(ctx, task.OnSuccess(postRequest, task.Close(serverWriter)), getResponse); err != nil {
		common.Interrupt(serverReader)
		common.Interrupt(serverWriter)
		return errors.New("connection ends").Base(err).AtInfo()
	}

	return nil
}
