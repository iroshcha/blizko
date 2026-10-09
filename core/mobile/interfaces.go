package mobile

import (
	"encoding/json"
	"net"
	"runtime"
	"sync"
	"tailscale.com/net/netmon"
)

var ifaceMu sync.RWMutex
var ifaceList []netmon.Interface

func init() {
	if runtime.GOOS == "android" {
		netmon.RegisterInterfaceGetter(func() ([]netmon.Interface, error) {
			ifaceMu.RLock()
			defer ifaceMu.RUnlock()
			return append([]netmon.Interface(nil), ifaceList...), nil
		})
	}
}

// SetInterfaces avoids Android's restricted netlink calls. Call before Start
// and on network changes with interfaces obtained from Android's Java API.
func SetInterfaces(raw string) error {
	var input []struct {
		Index     int      `json:"index"`
		Name      string   `json:"name"`
		MTU       int      `json:"mtu"`
		Up        bool     `json:"up"`
		Loopback  bool     `json:"loopback"`
		Addresses []string `json:"addresses"`
	}
	if e := json.Unmarshal([]byte(raw), &input); e != nil {
		return e
	}
	out := make([]netmon.Interface, 0, len(input))
	for _, v := range input {
		flags := net.FlagMulticast
		if v.Up {
			flags |= net.FlagUp | net.FlagRunning
		}
		if v.Loopback {
			flags |= net.FlagLoopback
		}
		addrs := []net.Addr{}
		for _, s := range v.Addresses {
			ip, p, e := net.ParseCIDR(s)
			if e == nil {
				p.IP = ip
				addrs = append(addrs, p)
			}
		}
		out = append(out, netmon.Interface{Interface: &net.Interface{Index: v.Index, Name: v.Name, MTU: v.MTU, Flags: flags}, AltAddrs: addrs})
	}
	ifaceMu.Lock()
	ifaceList = out
	ifaceMu.Unlock()
	return nil
}
