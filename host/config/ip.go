package config

import (
	"errors"
	"net"

	"github.com/docker/libcontainer/netlink"
)

func DefaultExternalIP() (string, error) {
	routes, err := netlink.NetworkGetRoutes()
	if err != nil {
		return "", err
	}

	var iface *net.Interface
	for _, r := range routes {
		if r.Default {
			iface = r.Iface
			break
		}
	}
	if iface == nil {
		return "", errors.New("config: Unable to identify default network interface")
	}

	addrs, err := iface.Addrs()
	if err != nil {
		return "", err
	}
	if len(addrs) == 0 {
		return "", errors.New("config: No IPs configured on default interface")
	}
	var defaultIP net.IP
	for _, a := range addrs {
		ip, _, _ := net.ParseCIDR(a.String())
		if ip.To4() != nil && ip.IsGlobalUnicast() {
			defaultIP = ip
			break
		}
	}
	if defaultIP == nil {
		return "", errors.New("config: Unable to determine default IPv4 address")
	}
	return defaultIP.String(), nil
}
