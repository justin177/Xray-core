package conf

import (
	"os"
	"strconv"
)

type LeanCloudAppPort struct {
}

func (x LeanCloudAppPort) Process(conf *Config) error {
	portStr := os.Getenv("LEANCLOUD_APP_PORT")
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
	RegisterConfigureFilePostProcessingStage("LeanCloudAppPort", &LeanCloudAppPort{})
}
