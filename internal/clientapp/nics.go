package clientapp

import (
	"net"

	"github.com/leganck/wakehub/internal/config"
)

// CollectNICs lists non-loopback, up interfaces with hardware addresses.
func CollectNICs() []config.NICInfo {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var out []config.NICInfo
	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback != 0 || len(iface.HardwareAddr) == 0 {
			continue
		}
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		info := config.NICInfo{Name: iface.Name, MAC: iface.HardwareAddr.String()}
		addrs, _ := iface.Addrs()
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok || ipn.IP.To4() == nil {
				continue
			}
			ip4 := ipn.IP.To4()
			info.IPv4 = append(info.IPv4, ip4.String())
			mask := ipn.Mask
			if len(mask) == 4 {
				b := make(net.IP, 4)
				for i := 0; i < 4; i++ {
					b[i] = ip4[i] | ^mask[i]
				}
				info.Broadcast = append(info.Broadcast, b.String())
			}
		}
		if info.MAC != "" {
			out = append(out, info)
		}
	}
	return out
}

// SanitizeInstance makes a string safe for mDNS instance names.
func SanitizeInstance(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' {
			out = append(out, r)
		} else {
			out = append(out, '-')
		}
	}
	if len(out) == 0 {
		return "client"
	}
	return string(out)
}
