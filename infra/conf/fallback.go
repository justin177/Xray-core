package conf

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"

	"github.com/xtls/xray-core/common/errors"
	"github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/common/protocol"
)

type InboundFallback struct {
	Name string          `json:"name"`
	Alpn string          `json:"alpn"`
	Path string          `json:"path"`
	Type string          `json:"type"`
	Dest json.RawMessage `json:"dest"`
	Xver uint64          `json:"xver"`

	PathType protocol.PathType `json:"pathType,omitempty"`
}

func buildFallbacks(parentProtocol string, in []*InboundFallback) (out []*protocol.Fallback, err error) {
	for _, fb := range in {
		var i uint16
		var s string
		if err := json.Unmarshal(fb.Dest, &i); err == nil {
			s = strconv.Itoa(int(i))
		} else {
			_ = json.Unmarshal(fb.Dest, &s)
		}
		out = append(out, &protocol.Fallback{
			Name: fb.Name,
			Alpn: fb.Alpn,
			Path: fb.Path,
			Type: fb.Type,
			Dest: s,
			Xver: fb.Xver,

			PathType: fb.PathType,
		})
	}
	for _, fb := range out {
		/*
			if fb.Alpn == "h2" && fb.Path != "" {
				return nil, errors.New(fmt.Sprintf(`%s fallbacks: "alpn":"h2" doesn't support "path"`, parentProtocol))
			}
		*/
		if fb.Path != "" && fb.Path[0] != '/' {
			return nil, errors.New(fmt.Sprintf(`%s fallbacks: "path" must be empty or start with "/"`, parentProtocol))
		}
		if fb.Type == "" && fb.Dest != "" {
			if fb.Dest == "serve-ws-none" {
				fb.Type = "serve"
			} else if filepath.IsAbs(fb.Dest) || fb.Dest[0] == '@' {
				fb.Type = "unix"
				if strings.HasPrefix(fb.Dest, "@@") && (runtime.GOOS == "linux" || runtime.GOOS == "android") {
					fullAddr := make([]byte, len(syscall.RawSockaddrUnix{}.Path)) // may need padding to work with haproxy
					copy(fullAddr, fb.Dest[1:])
					fb.Dest = string(fullAddr)
				}
			} else {
				if _, err := strconv.Atoi(fb.Dest); err == nil {
					fb.Dest = "localhost:" + fb.Dest
				}
				if _, _, err := net.SplitHostPort(fb.Dest); err == nil {
					fb.Type = "tcp"
				}
			}
		}
		if fb.Type == "" {
			return nil, errors.New(fmt.Sprintf(`%s fallbacks: please fill in a valid value for every "dest"`, parentProtocol))
		}
		if fb.Xver > 2 {
			return nil, errors.New(fmt.Sprintf(`%s fallbacks: invalid PROXY protocol version, "xver" only accepts 0, 1, 2`, parentProtocol))
		}
	}

	return out, nil
}
