package conf

import (
	"encoding/json"

	"github.com/xtls/xray-core/common/errors"
	"github.com/xtls/xray-core/common/serial"
	"github.com/xtls/xray-core/proxy/echo"
	"google.golang.org/protobuf/proto"
)

type EchoClientConfig struct {
	Response json.RawMessage `json:"response"`
}

func (v *EchoClientConfig) Build() (proto.Message, error) {
	config := new(echo.ClientConfig)
	if v.Response != nil {
		response, _, err := NewJSONConfigLoader(
			ConfigCreatorCache{
				"static_http": func() interface{} { return new(echo.StaticHttpResponse) },
				"get_http":    func() interface{} { return new(echo.GetHttpResponse) },
			},
			"type",
			"value").Load(v.Response)
		if err != nil {
			return nil, errors.New("Config: Failed to parse Blackhole response config.").Base(err)
		}
		responseSettings, err := response.(Buildable).Build()
		if err != nil {
			return nil, err
		}
		config.Response = serial.ToTypedMessage(responseSettings)
	}
	return config, nil
}

type EchoServerConfig struct {
	Response json.RawMessage `json:"response"`
}

func (v *EchoServerConfig) Build() (proto.Message, error) {
	config := new(echo.ServerConfig)
	if v.Response != nil {
		response, _, err := NewJSONConfigLoader(
			ConfigCreatorCache{
				"static_http": func() interface{} { return new(echo.StaticHttpResponse) },
				"get_http":    func() interface{} { return new(echo.GetHttpResponse) },
			},
			"type",
			"value").Load(v.Response)
		if err != nil {
			return nil, errors.New("Config: Failed to parse Blackhole response config.").Base(err)
		}
		responseSettings, err := response.(Buildable).Build()
		if err != nil {
			return nil, err
		}
		config.Response = serial.ToTypedMessage(responseSettings)
	}
	return config, nil
}
