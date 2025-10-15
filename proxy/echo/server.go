package echo

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net/http"

	"github.com/xtls/xray-core/common"
	"github.com/xtls/xray-core/common/buf"
	"github.com/xtls/xray-core/common/errors"
	"github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/features/routing"
	"github.com/xtls/xray-core/transport/internet/stat"
)

type Server struct {
	response ResponseConfig
}

func NewServer(ctx context.Context, config *ServerConfig) (*Server, error) {
	response, err := config.GetInternalResponse()
	if err != nil {
		return nil, err
	}
	return &Server{
		response: response,
	}, nil
}

// Network implements proxy.Inbound.
func (*Server) Network() []net.Network {
	return []net.Network{net.Network_TCP}
}

// Process implements OutboundServer.Dispatch().
func (h *Server) Process(ctx context.Context, network net.Network, conn stat.Connection, _ routing.Dispatcher) error {
	reader := bufio.NewReaderSize(conn, buf.Size)
	var body *bytes.Buffer
	request, err := http.ReadRequest(reader)
	if err == nil {
		body = bytes.NewBuffer(make([]byte, 0, request.ContentLength))
		_, err = io.Copy(body, request.Body)
	}
	if err != nil {
		trace := errors.New("failed to read http request").Base(err)
		if errors.Cause(err) != io.EOF && !isTimeout(errors.Cause(err)) {
			trace.AtWarning()
		}
		return trace
	}
	errors.LogInfo(ctx, "request to Method [", request.Method, "] Host [", request.Host, "] with URL [", request.URL, "]")

	_, err = h.response.WriteToClient(request, conn)
	return err
}

type String struct {
	*bytes.Buffer
}

func (x String) Close() error {
	return nil
}

func isTimeout(err error) bool {
	nerr, ok := errors.Cause(err).(net.Error)
	return ok && nerr.Timeout()
}

func init() {
	common.Must(common.RegisterConfig((*ServerConfig)(nil), func(ctx context.Context, config interface{}) (interface{}, error) {
		return NewServer(ctx, config.(*ServerConfig))
	}))
}
