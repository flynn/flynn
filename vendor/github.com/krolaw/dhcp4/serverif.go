package dhcp4

import (
	"net"

	"golang.org/x/net/ipv4"
)

type serveIfConn struct {
	ifIndex int
	conn    *ipv4.PacketConn
	cm      *ipv4.ControlMessage
}

func (s *serveIfConn) ReadFrom(b []byte) (n int, addr net.Addr, err error) {
	n, s.cm, addr, err = s.conn.ReadFrom(b)
	if s.cm != nil && s.cm.IfIndex != s.ifIndex { // Filter all other interfaces
		n = 0 // Packets < 240 are filtered in Serve().
	}
	return
}

func (s *serveIfConn) WriteTo(b []byte, addr net.Addr) (n int, err error) {

	// ipv4 docs state that Src is "specify only", however testing by tfheen
	// shows that Src IS populated.  Therefore, to reuse the control message,
	// we set Src to nil to avoid the error "write udp4: invalid argument"
	s.cm.Src = nil

	return s.conn.WriteTo(b, s.cm, addr)
}

// ServeIf does the same job as Serve(), but listens and responds on the
// specified network interface (by index).  It also doubles as an example of
// how to leverage the dhcp4.ServeConn interface.
//
// If your target only has one interface, use Serve(). ServeIf() requires an
// import outside the std library.  Serving DHCP over multiple interfaces will
// require your own dhcp4.ServeConn, as listening to broadcasts utilises all
// interfaces (so you cannot have more than on listener).
func ServeIf(ifIndex int, conn net.PacketConn, handler Handler) error {
	p := ipv4.NewPacketConn(conn)
	if err := p.SetControlMessage(ipv4.FlagInterface, true); err != nil {
		return err
	}
	return Serve(&serveIfConn{ifIndex: ifIndex, conn: p}, handler)
}

// ListenAndServe listens on the UDP network address addr and then calls
// Serve with handler to handle requests on incoming packets.
// i.e. ListenAndServeIf("eth0",handler)
func ListenAndServeIf(interfaceName string, handler Handler) error {
	iface, err := net.InterfaceByName(interfaceName)
	if err != nil {
		return err
	}
	l, err := net.ListenPacket("udp4", ":67")
	if err != nil {
		return err
	}
	defer l.Close()
	return ServeIf(iface.Index, l, handler)
}
