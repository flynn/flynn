package main_test

import (
	"bytes"
	"io"
	"os"
	"reflect"
	"testing"

	"github.com/flynn/flynn/discoverd"
)

// Ensures the CLI flags can be parsed.
func TestMain_ParseFlags(t *testing.T) {
	m := NewMain()
	opt, err := m.ParseFlags(
		"-data-dir", "/tmp/data/dir",
		"-host", "127.0.0.1",
		"-addr", "127.0.0.1:1000",
		"-dns-addr", "127.0.0.1:2000",
		"-recursors", "7.7.7.7,6.6.6.6",
		"-notify", "localhost",
		"-peers", "server0:3000,server1:3000,server2:3000",
	)
	if err != nil {
		t.Fatal(err)
	}

	// Verify parsed options.
	if opt.DataDir != "/tmp/data/dir" {
		t.Fatalf("unexpected data dir: %s", opt.DataDir)
	} else if opt.Addr != "127.0.0.1:1000" {
		t.Fatalf("unexpected addr: %s", opt.Addr)
	} else if opt.DNSAddr != "127.0.0.1:2000" {
		t.Fatalf("unexpected dns addr: %s", opt.DNSAddr)
	} else if !reflect.DeepEqual(opt.Recursors, []string{"7.7.7.7", "6.6.6.6"}) {
		t.Fatalf("unexpected recursors: %+v", opt.Recursors)
	} else if opt.Notify != "localhost" {
		t.Fatalf("unexpected notify: %s", opt.Notify)
	} else if !reflect.DeepEqual(opt.Peers, []string{"server0:3000", "server1:3000", "server2:3000"}) {
		t.Fatalf("unexpected peers: %s", opt.Peers)
	}
}

// Ensure a slice of hostports can have their ports updated.
func TestSetPortSlice(t *testing.T) {
	if a, err := main.SetPortSlice([]string{"host0:123", "host1:456", "host2:789"}, ":1000"); err != nil {
		t.Fatal(err)
	} else if !reflect.DeepEqual(a, []string{"host0:1000", "host1:1000", "host2:1000"}) {
		t.Fatalf("unexpected value: %+v", a)
	}
}

// Main represents a test wrapper for main.Main.
type Main struct {
	*main.Main

	Stdout bytes.Buffer
	Stderr bytes.Buffer
}

// NewMain returns a new instance of Main.
func NewMain() *Main {
	m := &Main{Main: main.NewMain()}
	if testing.Verbose() {
		m.Main.Stdout = io.MultiWriter(os.Stdout, &m.Stdout)
		m.Main.Stderr = io.MultiWriter(os.Stderr, &m.Stderr)
	} else {
		m.Main.Stdout = &m.Stdout
		m.Main.Stderr = &m.Stderr
	}
	return m
}
