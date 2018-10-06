package dhcp4

import (
	"net"

	"github.com/krolaw/dhcp4/conn"

	"golang.org/x/net/ipv4"
)

// Deprecated, use Serve instead with connection from dhcp4/conn or own custom creation
func ServeIf(ifIndex int, pconn net.PacketConn, handler Handler) error {
	p := ipv4.NewPacketConn(pconn)
	if err := p.SetControlMessage(ipv4.FlagInterface, true); err != nil {
		return err
	}
	return Serve(conn.NewServeIf(ifIndex, p), handler)
}

// Deprecated, use Serve instead with connection from dhcp4/conn or own custom creation
func ListenAndServeIf(interfaceName string, handler Handler) error {
	l, err := conn.NewUDP4FilterListener(interfaceName, ":67")
	if err != nil {
		return err
	}
	defer l.Close()
	return Serve(l, handler)
}
