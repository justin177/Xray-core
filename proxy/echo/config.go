package echo

import (
	"io"
	"net/http"

	"github.com/xtls/xray-core/common/buf"
)

type ResponseConfig interface {
	WriteToClient(*http.Request, io.Writer) (int, error)
	WriteToServer(*http.Request, buf.Writer) (int, error)
}

func (c *ClientConfig) GetInternalResponse() (ResponseConfig, error) {
	if c.GetResponse() == nil {
		return defaultResponse, nil
	}

	config, err := c.GetResponse().GetInstance()
	if err != nil {
		return nil, err
	}
	return config.(ResponseConfig), nil
}

func (c *ServerConfig) GetInternalResponse() (ResponseConfig, error) {
	if c.GetResponse() == nil {
		return defaultResponse, nil
	}

	config, err := c.GetResponse().GetInstance()
	if err != nil {
		return nil, err
	}
	return config.(ResponseConfig), nil
}

var defaultResponse = &StaticHttpResponse{
	Version: "1.1",
	Status:  "200",
	Reason:  "OK",
}
