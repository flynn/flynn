package ports

import (
	"net"
	"sync"

	"github.com/flynn/flynn/pkg/iptables"
)

type Forwarder struct {
	l sync.Mutex
	c *iptables.Chain

	host net.IP
}

func NewForwarder(host net.IP, c *iptables.Chain) *Forwarder {
	return &Forwarder{c: c, host: host}
}

func (f *Forwarder) Add(dest net.Addr, rangeEnd int, proto string) error {
	ip, port := ipAndPort(dest)
	return f.c.Forward(iptables.Add, f.host, port, rangeEnd, proto, ip.String())
}

func (f *Forwarder) Remove(dest net.Addr, rangeEnd int, proto string) error {
	ip, port := ipAndPort(dest)
	return f.c.Forward(iptables.Delete, f.host, port, rangeEnd, proto, ip.String())
}

func ipAndPort(a net.Addr) (net.IP, int) {
	switch t := a.(type) {
	case *net.TCPAddr:
		return t.IP, t.Port
	case *net.UDPAddr:
		return t.IP, t.Port
	}
	return nil, 0
}
