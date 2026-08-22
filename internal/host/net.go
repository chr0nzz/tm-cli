package host

import (
	"context"
	"net"
	"strings"
	"time"
)

func PrimaryIP() string {
	if HasCommand("hostname") {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if out, err := Output(Command(ctx, "hostname", "-I")); err == nil {
			if fields := strings.Fields(out); len(fields) > 0 {
				return fields[0]
			}
		}
	}
	for _, ip := range localIPs() {
		if ip.To4() != nil && !ip.IsLoopback() {
			return ip.String()
		}
	}
	return ""
}

func PublicIPs() []string {
	return filterPublic(localIPs())
}

func localIPs() []net.IP {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var ips []net.IP
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			var ip net.IP
			switch v := a.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip != nil {
				ips = append(ips, ip)
			}
		}
	}
	return ips
}

var cgnat = net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}

func filterPublic(ips []net.IP) []string {
	var out []string
	for _, ip := range ips {
		if ip.IsLoopback() || ip.IsUnspecified() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() {
			continue
		}
		if v4 := ip.To4(); v4 != nil && cgnat.Contains(v4) {
			continue
		}
		out = append(out, ip.String())
	}
	return out
}
