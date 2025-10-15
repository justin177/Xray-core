package fallback

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/xtls/xray-core/common"
	"github.com/xtls/xray-core/common/buf"
	"github.com/xtls/xray-core/common/errors"
	"github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/common/protocol"
	"github.com/xtls/xray-core/common/retry"
	"github.com/xtls/xray-core/common/signal"
	"github.com/xtls/xray-core/common/task"
	"github.com/xtls/xray-core/features/policy"
	"github.com/xtls/xray-core/transport/internet/reality"
	"github.com/xtls/xray-core/transport/internet/stat"
	"github.com/xtls/xray-core/transport/internet/tls"
)

type Handler struct {
	fallbacks map[string]map[string]map[string]*protocol.Fallback // or nil
}

func New(config *Config) *Handler {
	handler := &Handler{}

	if config.Fallbacks != nil {
		handler.fallbacks = make(map[string]map[string]map[string]*protocol.Fallback)
		for _, fb := range config.Fallbacks {
			if handler.fallbacks[fb.Name] == nil {
				handler.fallbacks[fb.Name] = make(map[string]map[string]*protocol.Fallback)
			}
			if handler.fallbacks[fb.Name][fb.Alpn] == nil {
				handler.fallbacks[fb.Name][fb.Alpn] = make(map[string]*protocol.Fallback)
			}
			handler.fallbacks[fb.Name][fb.Alpn][fb.Path] = fb
		}
		if handler.fallbacks[""] != nil {
			for name, apfb := range handler.fallbacks {
				if name != "" {
					for alpn := range handler.fallbacks[""] {
						if apfb[alpn] == nil {
							apfb[alpn] = make(map[string]*protocol.Fallback)
						}
					}
				}
			}
		}
		for _, apfb := range handler.fallbacks {
			if apfb[""] != nil {
				for alpn, pfb := range apfb {
					if alpn != "" { // && alpn != "h2" {
						for path, fb := range apfb[""] {
							if pfb[path] == nil {
								pfb[path] = fb
							}
						}
					}
				}
			}
		}
		if handler.fallbacks[""] != nil {
			for name, apfb := range handler.fallbacks {
				if name != "" {
					for alpn, pfb := range handler.fallbacks[""] {
						for path, fb := range pfb {
							if apfb[alpn][path] == nil {
								apfb[alpn][path] = fb
							}
						}
					}
				}
			}
		}
	}

	return handler
}

func (h *Handler) Process(ctx context.Context, err error, sessionPolicy policy.Session, connection stat.Connection, iConn stat.Connection, first *buf.Buffer, firstLen int64, reader buf.Reader) error {
	if err := connection.SetReadDeadline(time.Time{}); err != nil {
		errors.LogWarningInner(ctx, err, "unable to set back read deadline")
	}
	errors.LogInfoInner(ctx, err, "fallback starts")

	name := ""
	alpn := ""
	if tlsConn, ok := iConn.(*tls.Conn); ok {
		cs := tlsConn.ConnectionState()
		name = cs.ServerName
		alpn = cs.NegotiatedProtocol
		errors.LogInfo(ctx, "realName = "+name)
		errors.LogInfo(ctx, "realAlpn = "+alpn)
	} else if realityConn, ok := iConn.(*reality.Conn); ok {
		cs := realityConn.ConnectionState()
		name = cs.ServerName
		alpn = cs.NegotiatedProtocol
		errors.LogInfo(ctx, "realName = "+name)
		errors.LogInfo(ctx, "realAlpn = "+alpn)
	}
	name = strings.ToLower(name)
	alpn = strings.ToLower(alpn)

	napfb := h.fallbacks
	if len(napfb) > 1 || napfb[""] == nil {
		if name != "" && napfb[name] == nil {
			match := ""
			for n := range napfb {
				if n != "" && strings.Contains(name, n) && len(n) > len(match) {
					match = n
				}
			}
			name = match
		}
	}

	if napfb[name] == nil {
		name = ""
	}
	apfb := napfb[name]
	if apfb == nil {
		return errors.New(`failed to find the default "name" config`).AtWarning()
	}

	if apfb[alpn] == nil {
		alpn = ""
	}
	pfb := apfb[alpn]
	if pfb == nil {
		return errors.New(`failed to find the default "alpn" config`).AtWarning()
	}

	path := h.matchPath(ctx, first, firstLen, pfb)
	if pfb[path] == nil {
		path = ""
	}
	fb := pfb[path]
	if fb == nil {
		return errors.New(`failed to find the default "path" config`).AtWarning()
	}

	ctx, cancel := context.WithCancel(ctx)
	timer := signal.CancelAfterInactivity(ctx, cancel, sessionPolicy.Timeouts.ConnectionIdle)
	ctx = policy.ContextWithBufferPolicy(ctx, sessionPolicy.Buffer)

	var conn net.Conn
	if err := retry.ExponentialBackoff(5, 100).On(func() error {
		var dialer net.Dialer
		conn, err = dialer.DialContext(ctx, fb.Type, fb.Dest)
		if err != nil {
			return err
		}
		return nil
	}); err != nil {
		return errors.New("failed to dial to " + fb.Dest).Base(err).AtWarning()
	}
	defer conn.Close()

	serverReader := buf.NewReader(conn)
	serverWriter := buf.NewWriter(conn)

	postRequest := func() error {
		defer timer.SetTimeout(sessionPolicy.Timeouts.DownlinkOnly)
		if fb.Xver != 0 {
			ipType := 4
			remoteAddr, remotePort, err := net.SplitHostPort(connection.RemoteAddr().String())
			if err != nil {
				ipType = 0
			}
			localAddr, localPort, err := net.SplitHostPort(connection.LocalAddr().String())
			if err != nil {
				ipType = 0
			}
			if ipType == 4 {
				for i := 0; i < len(remoteAddr); i++ {
					if remoteAddr[i] == ':' {
						ipType = 6
						break
					}
				}
			}
			pro := buf.New()
			defer pro.Release()
			switch fb.Xver {
			case 1:
				if ipType == 0 {
					common.Must2(pro.Write([]byte("PROXY UNKNOWN\r\n")))
					break
				}
				if ipType == 4 {
					common.Must2(pro.Write([]byte("PROXY TCP4 " + remoteAddr + " " + localAddr + " " + remotePort + " " + localPort + "\r\n")))
				} else {
					common.Must2(pro.Write([]byte("PROXY TCP6 " + remoteAddr + " " + localAddr + " " + remotePort + " " + localPort + "\r\n")))
				}
			case 2:
				common.Must2(pro.Write([]byte("\x0D\x0A\x0D\x0A\x00\x0D\x0A\x51\x55\x49\x54\x0A"))) // signature
				if ipType == 0 {
					common.Must2(pro.Write([]byte("\x20\x00\x00\x00"))) // v2 + LOCAL + UNSPEC + UNSPEC + 0 bytes
					break
				}
				if ipType == 4 {
					common.Must2(pro.Write([]byte("\x21\x11\x00\x0C"))) // v2 + PROXY + AF_INET + STREAM + 12 bytes
					common.Must2(pro.Write(net.ParseIP(remoteAddr).To4()))
					common.Must2(pro.Write(net.ParseIP(localAddr).To4()))
				} else {
					common.Must2(pro.Write([]byte("\x21\x21\x00\x24"))) // v2 + PROXY + AF_INET6 + STREAM + 36 bytes
					common.Must2(pro.Write(net.ParseIP(remoteAddr).To16()))
					common.Must2(pro.Write(net.ParseIP(localAddr).To16()))
				}
				p1, _ := strconv.ParseUint(remotePort, 10, 16)
				p2, _ := strconv.ParseUint(localPort, 10, 16)
				common.Must2(pro.Write([]byte{byte(p1 >> 8), byte(p1), byte(p2 >> 8), byte(p2)}))
			}
			if err := serverWriter.WriteMultiBuffer(buf.MultiBuffer{pro}); err != nil {
				return errors.New("failed to set PROXY protocol v", fb.Xver).Base(err).AtWarning()
			}
		}
		if err := buf.Copy(reader, serverWriter, buf.UpdateActivity(timer)); err != nil {
			return errors.New("failed to fallback request payload").Base(err).AtInfo()
		}
		return nil
	}

	writer := buf.NewWriter(connection)

	getResponse := func() error {
		defer timer.SetTimeout(sessionPolicy.Timeouts.UplinkOnly)
		if err := buf.Copy(serverReader, writer, buf.UpdateActivity(timer)); err != nil {
			return errors.New("failed to deliver response payload").Base(err).AtInfo()
		}
		return nil
	}

	if err := task.Run(ctx, task.OnSuccess(postRequest, task.Close(serverWriter)), task.OnSuccess(getResponse, task.Close(writer))); err != nil {
		common.Must(common.Interrupt(serverReader))
		common.Must(common.Interrupt(serverWriter))
		return errors.New("fallback ends").Base(err).AtInfo()
	}

	return nil
}
func (h *Handler) matchPath(ctx context.Context, first *buf.Buffer, firstLen int64, pfb map[string]*protocol.Fallback) string {
	path := ""
	if len(pfb) > 1 || pfb[""] == nil {
		if firstLen >= 18 && first.Byte(4) != '*' { // not h2c
			firstBytes := first.Bytes()
			for i := 4; i <= 8; i++ { // 5 -> 9
				if firstBytes[i] == '/' && firstBytes[i-1] == ' ' {
					search := len(firstBytes)
					if search > 64 {
						search = 64 // up to about 60
					}
					for j := i + 1; j < search; j++ {
						k := firstBytes[j]
						if k == '\r' || k == '\n' { // avoid logging \r or \n
							break
						}
						if k == '?' || k == ' ' {
							path = string(firstBytes[i:j])
							errors.LogInfo(ctx, "realPath = "+path)
							match := ""
							for _, fb := range pfb {
								if fb.PathType == protocol.PathType_Exact {
									if fb.Path == path {
										match = fb.Path
										break
									}
								} else if fb.PathType == protocol.PathType_Prefix {
									if fb.Path != "" && strings.HasPrefix(path, fb.Path) && len(fb.Path) > len(match) {
										match = fb.Path
									}
								} else if fb.PathType == protocol.PathType_Regexp {
									if fb.Path != "" && regexpMatchString(fb.Path, path) && len(fb.Path) > len(match) {
										match = fb.Path
									}
								}
							}
							path = match
							break
						}
					}
					break
				}
			}
		}
	}
	return path
}

func regexpMatchString(pattern string, s string) bool {
	matched, _ := regexp.MatchString(pattern, s)
	return matched
}
