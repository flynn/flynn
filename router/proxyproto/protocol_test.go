package proxyproto

import (
	"bytes"
	"net"
	"testing"
)

func TestPassthrough(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	pl := &Listener{Listener: l}
	defer pl.Close()

	go func() {
		conn, err := net.Dial("tcp", pl.Addr().String())
		if err != nil {
			t.Errorf("err: %v", err)
			return
		}
		defer conn.Close()

		conn.Write([]byte("ping"))
		recv := make([]byte, 4)
		_, err = conn.Read(recv)
		if err != nil {
			t.Errorf("err: %v", err)
			return
		}
		if !bytes.Equal(recv, []byte("pong")) {
			t.Errorf("bad: %v", recv)
			return
		}
	}()

	conn, err := pl.Accept()
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	defer conn.Close()

	recv := make([]byte, 4)
	_, err = conn.Read(recv)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !bytes.Equal(recv, []byte("ping")) {
		t.Fatalf("bad: %v", recv)
	}

	if _, err := conn.Write([]byte("pong")); err != nil {
		t.Fatalf("err: %v", err)
	}
}

func TestParse_ipv4(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	pl := &Listener{Listener: l}
	defer pl.Close()

	go func() {
		conn, err := net.Dial("tcp", pl.Addr().String())
		if err != nil {
			t.Errorf("err: %v", err)
			return
		}
		defer conn.Close()

		// Write out the header!
		header := "PROXY TCP4 10.1.1.1 20.2.2.2 1000 2000\r\nfoo"
		conn.Write([]byte(header))

		conn.Write([]byte("ping"))
		recv := make([]byte, 4)
		_, err = conn.Read(recv)
		if err != nil {
			t.Errorf("err: %v", err)
			return
		}
		if !bytes.Equal(recv, []byte("pong")) {
			t.Errorf("bad: %v", recv)
			return
		}
	}()

	conn, err := pl.Accept()
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	defer conn.Close()

	recv := make([]byte, 7)
	_, err = conn.Read(recv)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !bytes.Equal(recv, []byte("fooping")) {
		t.Fatalf("bad: %v", recv)
	}

	if _, err := conn.Write([]byte("pong")); err != nil {
		t.Fatalf("err: %v", err)
	}

	// Check the remote addr
	addr := conn.RemoteAddr().(*net.TCPAddr)
	if addr.IP.String() != "10.1.1.1" {
		t.Fatalf("bad: %v", addr)
	}
	if addr.Port != 1000 {
		t.Fatalf("bad: %v", addr)
	}
}

func TestParse_ipv6(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	pl := &Listener{Listener: l}
	defer pl.Close()

	go func() {
		conn, err := net.Dial("tcp", pl.Addr().String())
		if err != nil {
			t.Errorf("err: %v", err)
			return
		}
		defer conn.Close()

		// Write out the header!
		header := "PROXY TCP6 ffff::ffff ffff::ffff 1000 2000\r\n"
		conn.Write([]byte(header))

		conn.Write([]byte("ping"))
		recv := make([]byte, 4)
		_, err = conn.Read(recv)
		if err != nil {
			t.Errorf("err: %v", err)
			return
		}
		if !bytes.Equal(recv, []byte("pong")) {
			t.Errorf("bad: %v", recv)
			return
		}
	}()

	conn, err := pl.Accept()
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	defer conn.Close()

	recv := make([]byte, 4)
	_, err = conn.Read(recv)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !bytes.Equal(recv, []byte("ping")) {
		t.Fatalf("bad: %v", recv)
	}

	if _, err := conn.Write([]byte("pong")); err != nil {
		t.Fatalf("err: %v", err)
	}

	// Check the remote addr
	addr := conn.RemoteAddr().(*net.TCPAddr)
	if addr.IP.String() != "ffff::ffff" {
		t.Fatalf("bad: %v", addr)
	}
	if addr.Port != 1000 {
		t.Fatalf("bad: %v", addr)
	}
}

func TestParse_BadHeader(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	pl := &Listener{Listener: l}
	defer pl.Close()

	go func() {
		conn, err := net.Dial("tcp", pl.Addr().String())
		if err != nil {
			t.Errorf("err: %v", err)
			return
		}
		defer conn.Close()

		conn.Write([]byte("PROXY TCP4 what 127.0.0.1 1000 2000\r\nping"))
	}()

	conn, err := pl.Accept()
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	defer conn.Close()

	// Check the remote addr, should be the local addr
	addr := conn.RemoteAddr().(*net.TCPAddr)
	if addr.IP.String() != "127.0.0.1" {
		t.Fatalf("bad: %v", addr)
	}

	// Read should fail
	recv := make([]byte, 4)
	_, err = conn.Read(recv)
	if err == nil {
		t.Fatalf("err: %v", err)
	}
}
