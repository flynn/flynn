package main

import (
	"io"
	"io/ioutil"
	"net"
	"strconv"

	"github.com/flynn/strowger/types"
	. "github.com/titanous/gocheck"
)

func NewTCPTestServer(prefix string) *TCPTestServer {
	s := &TCPTestServer{prefix: prefix}
	var err error
	s.l, err = net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	s.Addr = s.l.Addr().String()
	go s.Serve()
	return s
}

type TCPTestServer struct {
	Addr   string
	prefix string
	l      net.Listener
}

func (s *TCPTestServer) Serve() {
	for {
		conn, err := s.l.Accept()
		if err != nil {
			return
		}
		go func() {
			conn.Write([]byte(s.prefix))
			io.Copy(conn, conn)
			conn.Close()
		}()
	}
}

func (s *TCPTestServer) Close() error { return s.l.Close() }

const firstTCPPort, lastTCPPort = 10000, 10009

func newTCPListener(etcd *fakeEtcd) (*TCPListener, *fakeDiscoverd, error) {
	discoverd := newFakeDiscoverd()
	if etcd == nil {
		etcd = newFakeEtcd()
	}
	l := NewTCPListener("127.0.0.1", firstTCPPort, lastTCPPort, NewEtcdDataStore(etcd, "/strowger/tcp/"), discoverd)
	return l, discoverd, l.Start()
}

func assertTCPConn(c *C, addr, prefix string) {
	conn, err := net.Dial("tcp", addr)
	c.Assert(err, IsNil)
	conn.Write([]byte("asdf"))
	conn.(*net.TCPConn).CloseWrite()
	res, err := ioutil.ReadAll(conn)
	conn.Close()

	c.Assert(err, IsNil)
	c.Assert(string(res), Equals, prefix+"asdf")
}

func (s *S) TestAddTCPRoute(c *C) {
	const addr, port, portInt = "127.0.0.1:45000", "45000", 45000
	srv1 := NewTCPTestServer("1")
	srv2 := NewTCPTestServer("2")
	defer srv1.Close()
	defer srv2.Close()

	l, discoverd, err := newTCPListener(nil)
	c.Assert(err, IsNil)
	defer l.Close()

	discoverd.Register("test", srv1.Addr)
	defer discoverd.UnregisterAll()

	wait := waitForEvent(c, l, "add", port)
	err = l.AddRoute(&strowger.TCPRoute{Port: portInt, Service: "test"})
	c.Assert(err, IsNil)
	wait()

	assertTCPConn(c, addr, "1")

	discoverd.Unregister("test", srv1.Addr)
	discoverd.Register("test", srv2.Addr)

	assertTCPConn(c, addr, "2")

	wait = waitForEvent(c, l, "remove", port)
	err = l.RemoveRoute(port)
	c.Assert(err, IsNil)
	wait()

	_, err = net.Dial("tcp", addr)
	c.Assert(err, Not(IsNil))
}

func (s *S) TestInitialTCPSync(c *C) {
	const addr, port = "127.0.0.1:45000", 45000
	etcd := newFakeEtcd()
	l, _, err := newTCPListener(etcd)
	c.Assert(err, IsNil)
	wait := waitForEvent(c, l, "add", strconv.Itoa(port))
	err = l.AddRoute(&strowger.TCPRoute{Service: "test", Port: port})
	c.Assert(err, IsNil)
	wait()
	l.Close()

	srv := NewTCPTestServer("1")
	defer srv.Close()

	l, discoverd, err := newTCPListener(etcd)
	c.Assert(err, IsNil)
	defer l.Close()

	discoverd.Register("test", srv.Addr)
	defer discoverd.UnregisterAll()

	assertTCPConn(c, addr, "1")
}

func (s *S) TestTCPPortAllocation(c *C) {
	l, discoverd, err := newTCPListener(nil)
	c.Assert(err, IsNil)
	defer l.Close()
	for i := 0; i < 2; i++ {
		ports := make([]string, 0, 10)
		for j := 0; j < 10; j++ {
			route := &strowger.TCPRoute{Service: "test"}
			wait := waitForEvent(c, l, "add", "")
			err := l.AddRoute(route)
			c.Assert(err, IsNil)
			c.Assert(route.Port >= firstTCPPort && route.Port <= lastTCPPort, Equals, true)
			wait()

			port := strconv.Itoa(route.Port)
			ports = append(ports, port)
			srv := NewTCPTestServer(port)
			defer srv.Close()
			discoverd.Register("test", srv.Addr)

			assertTCPConn(c, "127.0.0.1:"+port, port)
			discoverd.UnregisterAll()
		}
		err = l.AddRoute(&strowger.TCPRoute{Service: "test"})
		c.Assert(err, Equals, ErrNoPorts)
		for _, port := range ports {
			wait := waitForEvent(c, l, "remove", port)
			l.RemoveRoute(port)
			wait()
		}
	}
}
