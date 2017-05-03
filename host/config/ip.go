package config

import (
	"errors"
	"net"

	"github.com/vishvananda/netlink"
)

func DefaultExternalIP() (string, error) {
	routes, err := netlink.RouteList(nil, netlink.FAMILY_V4)
	if err != nil {
		return "", err
	}

	var idx *int
	for _, r := range routes {
		if r.Dst == nil { // default route
			idx = &r.LinkIndex
			break
		}
	}
	if idx == nil {
		return "", errors.New("config: Unable to identify default network interface")
	}

	link, err := netlink.LinkByIndex(*idx)
	if err != nil {
		return "", err
	}
	addrs, err := netlink.AddrList(link, netlink.FAMILY_V4)
	if err != nil {
		return "", err
	}
	if len(addrs) == 0 {
		return "", errors.New("config: No IPs configured on default interface")
	}
	var defaultIP net.IP
	for _, a := range addrs {
		if a.IPNet.IP.IsGlobalUnicast() {
			defaultIP = a.IPNet.IP
			break
		}
	}
	if defaultIP == nil {
		return "", errors.New("config: Unable to determine default IPv4 address")
	}
	return defaultIP.String(), nil
}
