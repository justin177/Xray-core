package conf

import (
	"os"
	"strconv"
)

type GaeApplicationPort struct {
}

func (x GaeApplicationPort) Process(conf *Config) error {
	if _, found := os.LookupEnv("GAE_APPLICATION"); !found {
		return nil
	}

	portStr := os.Getenv("PORT")
	if len(portStr) == 0 {
		return nil
	}
	port, err := strconv.ParseUint(portStr, 10, 32)
	if err != nil {
		return err
	}
	if number := uint32(port); number > 0 && len(conf.InboundConfigs) > 0 {
		if conf.InboundConfigs[0].PortList == nil {
			conf.InboundConfigs[0].PortList = &PortList{}
		}
		conf.InboundConfigs[0].PortList.Range = append(conf.InboundConfigs[0].PortList.Range, PortRange{From: number, To: number})
	}
	return nil
}

func init() {
	RegisterConfigureFilePostProcessingStage("GaeApplicationPort", &GaeApplicationPort{})
}
