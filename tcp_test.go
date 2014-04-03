package main

import (
	"bytes"
	"io"
	"io/ioutil"
	"net"
	"strconv"
	"strings"

	"github.com/flynn/strowger/types"
	. "github.com/titanous/gocheck"
)

func NewTCPTestServer(r io.Reader, w io.Writer) *TCPTestServer {
	s := &TCPTestServer{w: w, r: r}
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
	Addr string
	w    io.Writer
	r    io.Reader
	l    net.Listener
}

func (s *TCPTestServer) Serve() {
	for {
		conn, err := s.l.Accept()
		if err != nil {
			return
		}
		go func() {
			defer conn.Close()
			done := make(chan struct{})
			go func() {
				io.Copy(conn, s.r)
				close(done)
			}()
			io.Copy(s.w, conn)
			<-done
		}()
	}
}

func (s *TCPTestServer) Close() error { return s.l.Close() }

func newTCPListener(etcd *fakeEtcd) (*TCPListener, *fakeDiscoverd, error) {
	discoverd := newFakeDiscoverd()
	if etcd == nil {
		etcd = newFakeEtcd()
	}
	l := NewTCPListener("127.0.0.1", NewEtcdDataStore(etcd, "/strowger/tcp/"), discoverd)
	return l, discoverd, l.Start()
}

func assertTCPConn(c *C, addr, expected string, rcvd *bytes.Buffer) {
	conn, err := net.Dial("tcp", addr)
	c.Assert(err, IsNil)
	conn.Write([]byte("asdf"))
	conn.(*net.TCPConn).CloseWrite()
	res, err := ioutil.ReadAll(conn)
	conn.Close()

	c.Assert(err, IsNil)
	c.Assert(string(res), Equals, expected)
	c.Assert(rcvd.String(), Equals, "asdf")
	rcvd.Reset()
}

func (s *S) TestAddTCPRoute(c *C) {
	const addr, port, portInt = "127.0.0.1:45000", "45000", 45000
	buf := &bytes.Buffer{}
	srv1 := NewTCPTestServer(strings.NewReader("1"), buf)
	srv2 := NewTCPTestServer(strings.NewReader("2"), buf)
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

	assertTCPConn(c, addr, "1", buf)

	discoverd.Unregister("test", srv1.Addr)
	discoverd.Register("test", srv2.Addr)

	assertTCPConn(c, addr, "2", buf)

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

	buf := &bytes.Buffer{}
	srv := NewTCPTestServer(strings.NewReader("1"), buf)
	defer srv.Close()

	l, discoverd, err := newTCPListener(etcd)
	c.Assert(err, IsNil)
	defer l.Close()

	discoverd.Register("test", srv.Addr)
	defer discoverd.UnregisterAll()

	assertTCPConn(c, addr, "1", buf)
}
