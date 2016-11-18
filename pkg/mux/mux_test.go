package mux_test

import (
	"bytes"
	"io"
	"io/ioutil"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/flynn/flynn/pkg/mux"
)

// Ensure the muxer can split a listener's connections across multiple listeners.
func TestMux_Listen(t *testing.T) {
	// Open single listener on random port.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	// Create muxer for listener.
	m := mux.New(ln)
	m.Timeout = 1 * time.Second
	if testing.Verbose() {
		m.LogOutput = os.Stderr
	}

	// Create listeners and begin serving mux.
	m.Listen([]byte{'\x00'})
	subln := m.Listen([]byte{'G', 'P', 'D'})
	go m.Serve()

	// Send message to listener.
	go func() {
		conn, err := net.Dial("tcp", ln.Addr().String())
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()

		// Write data & close.
		if _, err := conn.Write([]byte("GET")); err != nil {
			t.Error(err)
			return
		} else if err = conn.Close(); err != nil {
			t.Error(err)
			return
		}
	}()

	// Receive connection on appropriate listener.
	conn, err := subln.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	// Read message.
	if buf, err := ioutil.ReadAll(conn); err != nil {
		t.Fatal(err)
	} else if string(buf) != "GET" {
		t.Fatalf("unexpected message: %q", string(buf))
	} else if err = conn.Close(); err != nil {
		t.Fatal(err)
	}
}

// Ensure the muxer closes connections that don't have a registered header byte.
func TestMux_Listen_ErrUnregisteredHandler(t *testing.T) {
	// Open single listener on random port.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	// Write log output to a buffer to verify.
	var buf bytes.Buffer

	// Mux listener.
	m := mux.New(ln)
	m.Timeout = 1 * time.Second
	m.LogOutput = &buf
	if testing.Verbose() {
		m.LogOutput = io.MultiWriter(m.LogOutput, os.Stderr)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		m.Serve()
	}()

	// Send message to listener.
	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	// Write unregistered header byte.
	if _, err := conn.Write([]byte{'\x80'}); err != nil {
		t.Fatal(err)
	}

	// Connection should close immediately.
	if _, err := ioutil.ReadAll(conn); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	// Close connection and wait for server to finish.
	ln.Close()
	wg.Wait()

	// Verify error was logged.
	time.Sleep(100 * time.Millisecond)
	if s := buf.String(); !strings.Contains(s, `unregistered header byte: 0x80`) {
		t.Fatalf("unexpected log output:\n\n%s", s)
	}
}
