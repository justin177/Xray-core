package conf

import (
	"github.com/xtls/xray-core/common/errors"
	"github.com/xtls/xray-core/common/protocol"
	"github.com/xtls/xray-core/common/serial"
	"github.com/xtls/xray-core/proxy/trojan"
	"google.golang.org/protobuf/proto"
)

// TrojanServerTarget is configuration of a single trojan server
type TrojanServerTarget struct {
	Address  *Address `json:"address"`
	Port     uint16   `json:"port"`
	Password string   `json:"password"`
	Email    string   `json:"email"`
	Level    byte     `json:"level"`
	Flow     string   `json:"flow"`
}

// TrojanClientConfig is configuration of trojan servers
type TrojanClientConfig struct {
	Servers []*TrojanServerTarget `json:"servers"`
}

// Build implements Buildable
func (c *TrojanClientConfig) Build() (proto.Message, error) {
	if len(c.Servers) == 0 {
		return nil, errors.New("0 Trojan server configured.")
	}

	config := &trojan.ClientConfig{
		Server: make([]*protocol.ServerEndpoint, len(c.Servers)),
	}

	for idx, rec := range c.Servers {
		if rec.Address == nil {
			return nil, errors.New("Trojan server address is not set.")
		}
		if rec.Port == 0 {
			return nil, errors.New("Invalid Trojan port.")
		}
		if rec.Password == "" {
			return nil, errors.New("Trojan password is not specified.")
		}
		if rec.Flow != "" {
			return nil, errors.PrintRemovedFeatureError(`Flow for Trojan`, ``)
		}

		config.Server[idx] = &protocol.ServerEndpoint{
			Address: rec.Address.Build(),
			Port:    uint32(rec.Port),
			User: []*protocol.User{
				{
					Level: uint32(rec.Level),
					Email: rec.Email,
					Account: serial.ToTypedMessage(&trojan.Account{
						Password: rec.Password,
					}),
				},
			},
		}
	}

	return config, nil
}

// TrojanUserConfig is user configuration
type TrojanUserConfig struct {
	Password string `json:"password"`
	Level    byte   `json:"level"`
	Email    string `json:"email"`
	Flow     string `json:"flow"`
}

// TrojanServerConfig is Inbound configuration
type TrojanServerConfig struct {
	Clients   []*TrojanUserConfig `json:"clients"`
	Fallbacks []*InboundFallback  `json:"fallbacks"`
}

// Build implements Buildable
func (c *TrojanServerConfig) Build() (proto.Message, error) {
	config := &trojan.ServerConfig{
		Users: make([]*protocol.User, len(c.Clients)),
	}

	for idx, rawUser := range c.Clients {
		if rawUser.Flow != "" {
			return nil, errors.PrintRemovedFeatureError(`Flow for Trojan`, ``)
		}

		config.Users[idx] = &protocol.User{
			Level: uint32(rawUser.Level),
			Email: rawUser.Email,
			Account: serial.ToTypedMessage(&trojan.Account{
				Password: rawUser.Password,
			}),
		}
	}

	if v, err := buildFallbacks("Trojan", c.Fallbacks); err != nil {
		return nil, err
	} else {
		config.Fallbacks = v
	}

	return config, nil
}
