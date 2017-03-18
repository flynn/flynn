package main

import (
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"strconv"

	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/discoverd/testutil"
	"github.com/flynn/flynn/router/types"
	. "github.com/flynn/go-check"
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

func (s *S) newTCPListener(t testutil.TestingT) *TCPListener {
	l := &TCPListener{
		IP:        "127.0.0.1",
		ds:        NewPostgresDataStore("tcp", s.pgx),
		discoverd: s.discoverd,
	}
	l.startPort, l.endPort = allocatePortRange(10)
	if err := l.Start(); err != nil {
		t.Fatal(err)
	}

	return l
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
	portInt := allocatePort()
	port := strconv.Itoa(portInt)
	addr := "127.0.0.1:" + port

	srv1 := NewTCPTestServer("1")
	srv2 := NewTCPTestServer("2")
	defer srv1.Close()
	defer srv2.Close()

	l := s.newTCPListener(c)
	defer l.Close()

	r := addTCPRoute(c, l, portInt)

	unregister := discoverdRegisterTCP(c, l, srv1.Addr)

	assertTCPConn(c, addr, "1")

	unregister()
	discoverdRegisterTCP(c, l, srv2.Addr)

	assertTCPConn(c, addr, "2")

	wait := waitForEvent(c, l, "remove", r.ID)
	err := l.RemoveRoute(r.ID)
	c.Assert(err, IsNil)
	wait()

	_, err = net.Dial("tcp", addr)
	c.Assert(err, Not(IsNil))
}

func (s *S) TestAddTCPRouteReservedPort(c *C) {
	l := s.newTCPListener(c)
	defer l.Close()

	l.reservedPorts = []int{80, 443}

	for _, port := range l.reservedPorts {
		r := router.TCPRoute{Port: port}.ToRoute()
		err := l.AddRoute(r)
		c.Assert(err, NotNil)
		c.Assert(err.Error(), Equals, "router: cannot bind TCP to a reserved port")
	}
}

func addTCPRoute(c *C, l *TCPListener, port int) *router.TCPRoute {
	wait := waitForEvent(c, l, "set", "")
	r := router.TCPRoute{
		Service: "test",
		Port:    port,
	}.ToRoute()
	err := l.AddRoute(r)
	c.Assert(err, IsNil)
	wait()
	return r.TCPRoute()
}

func (s *S) TestTCPLeaderRouting(c *C) {
	portInt := allocatePort()
	port := strconv.Itoa(portInt)
	addr := "127.0.0.1:" + port

	srv1 := NewTCPTestServer("1")
	srv2 := NewTCPTestServer("2")
	defer srv1.Close()
	defer srv2.Close()

	l := s.newTCPListener(c)
	defer l.Close()

	client := l.discoverd
	err := client.AddService("leader-routing-tcp", &discoverd.ServiceConfig{
		LeaderType: discoverd.LeaderTypeManual,
	})
	c.Assert(err, IsNil)

	wait := waitForEvent(c, l, "set", "")
	r := router.TCPRoute{
		Service: "leader-routing-tcp",
		Port:    portInt,
		Leader:  true,
	}.ToRoute()
	err = l.AddRoute(r)
	c.Assert(err, IsNil)
	wait()

	discoverdRegisterTCPService(c, l, "leader-routing-tcp", srv1.Addr)
	discoverdRegisterTCPService(c, l, "leader-routing-tcp", srv2.Addr)

	discoverdSetLeaderTCP(c, l, "leader-routing-tcp", md5sum("tcp-"+srv1.Addr))
	assertTCPConn(c, addr, "1")

	discoverdSetLeaderTCP(c, l, "leader-routing-tcp", md5sum("tcp-"+srv2.Addr))
	assertTCPConn(c, addr, "2")
}

func (s *S) TestInitialTCPSync(c *C) {
	port := allocatePort()
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	l := s.newTCPListener(c)
	addTCPRoute(c, l, port)
	l.Close()

	srv := NewTCPTestServer("1")
	defer srv.Close()

	l = s.newTCPListener(c)
	defer l.Close()

	discoverdRegisterTCP(c, l, srv.Addr)

	assertTCPConn(c, addr, "1")
}

func (s *S) TestTCPPortAllocation(c *C) {
	l := s.newTCPListener(c)
	defer l.Close()
	for i := 0; i < 2; i++ {
		ports := make([]string, 0, 10)
		for j := 0; j < 10; j++ {
			route := addTCPRoute(c, l, 0)
			c.Assert(route.Port >= l.startPort && route.Port <= l.endPort, Equals, true)

			port := strconv.Itoa(route.Port)
			ports = append(ports, route.ID)
			srv := NewTCPTestServer(port)
			unregister := discoverdRegisterTCP(c, l, srv.Addr)

			assertTCPConn(c, "127.0.0.1:"+port, port)
			unregister()
			srv.Close()
		}
		r := router.TCPRoute{Service: "test"}.ToRoute()
		err := l.AddRoute(r)
		c.Assert(err, Equals, ErrNoPorts)
		for _, port := range ports {
			wait := waitForEvent(c, l, "remove", port)
			l.RemoveRoute(port)
			wait()
		}
	}
}
