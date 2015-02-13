package main

import (
	"io"
	"io/ioutil"
	"net"
	"strconv"

	. "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
	"github.com/flynn/flynn/discoverd/testutil/etcdrunner"
	"github.com/flynn/flynn/router/types"
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

const firstTCPPort, lastTCPPort = 10001, 10010

func (s *S) newTCPListener(t etcdrunner.TestingT) *TCPListener {
	l := &TCPListener{
		IP:        "127.0.0.1",
		startPort: firstTCPPort,
		endPort:   lastTCPPort,
		ds:        NewPostgresDataStore("tcp", s.pgx),
		discoverd: s.discoverd,
	}
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
	const addr, port, portInt = "127.0.0.1:45000", "45000", 45000
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

func (s *S) TestInitialTCPSync(c *C) {
	const addr, port = "127.0.0.1:45000", 45000
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
			c.Assert(route.Port >= firstTCPPort && route.Port <= lastTCPPort, Equals, true)

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
