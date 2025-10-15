package echo

import (
	"context"
	"time"

	"github.com/xtls/xray-core/common"
	"github.com/xtls/xray-core/common/errors"
	"github.com/xtls/xray-core/common/session"
	"github.com/xtls/xray-core/transport"
	"github.com/xtls/xray-core/transport/internet"
)

// Client is an outbound connection that silently swallow the entire payload.
type Client struct {
	response ResponseConfig
}

// New creates a new blackhole Client.
func NewClient(ctx context.Context, config *ClientConfig) (*Client, error) {
	response, err := config.GetInternalResponse()
	if err != nil {
		return nil, err
	}
	return &Client{
		response: response,
	}, nil
}

// Process implements OutboundHandler.Dispatch().
func (h *Client) Process(ctx context.Context, link *transport.Link, dialer internet.Dialer) error {
	outbounds := session.OutboundsFromContext(ctx)
	ob := outbounds[len(outbounds)-1]
	if !ob.Target.IsValid() {
		return errors.New("target not specified.")
	}
	ob.Name = "echo"

	nBytes, err := h.response.WriteToServer(nil, link.Writer)
	if err != nil {
		return err
	}
	if nBytes > 0 {
		// Sleep a little here to make sure the response is sent to client.
		time.Sleep(time.Second)
	}
	common.Interrupt(link.Writer)
	return nil
}

func init() {
	common.Must(common.RegisterConfig((*ClientConfig)(nil), func(ctx context.Context, config interface{}) (interface{}, error) {
		return NewClient(ctx, config.(*ClientConfig))
	}))
}
