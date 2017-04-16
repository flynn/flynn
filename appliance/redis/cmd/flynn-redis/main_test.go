package main_test

import (
	"bytes"
	"io"
	"io/ioutil"
	"os"
	"reflect"
	"testing"

	main "github.com/flynn/flynn/appliance/redis/cmd/flynn-redis"
	"github.com/flynn/flynn/discoverd/client"
)

// Ensure the program can register with discoverd.
func TestMain_Discoverd(t *testing.T) {
	m := NewMain()
	defer m.Close()

	// Mock heartbeater.
	var hbClosed bool
	hb := NewHeartbeater("127.0.0.1:0")
	hb.CloseFn = func() error { hbClosed = true; return nil }

	// Validate arguments passed to discoverd.
	m.DiscoverdClient.AddServiceFn = func(name string, config *discoverd.ServiceConfig) error {
		if name != "redis" {
			t.Fatalf("unexpected service name: %s", name)
		}
		return nil
	}
	m.DiscoverdClient.RegisterInstanceFn = func(service string, inst *discoverd.Instance) (discoverd.Heartbeater, error) {
		if service != "redis" {
			t.Fatalf("unexpected service: %s", service)
		} else if !reflect.DeepEqual(inst, &discoverd.Instance{
			Addr: ":6379",
			Meta: map[string]string{"REDIS_ID": m.Process.ID},
		}) {
			t.Fatalf("unexpected inst: %#v", inst)
		}
		return hb, nil
	}

	// set a password
	m.Process.Password = "test"

	// Execute program.
	if err := m.Run(); err != nil {
		t.Fatal(err)
	}

	// Close program and validate that the heartbeater was closed.
	if err := m.Close(); err != nil {
		t.Fatal(err)
	} else if !hbClosed {
		t.Fatal("expected heartbeater close")
	}
}

// Main is a test wrapper for main.Main.
type Main struct {
	*main.Main
	DiscoverdClient *DiscoverdClient

	Stdin  bytes.Buffer
	Stdout bytes.Buffer
	Stderr bytes.Buffer
}

// NewMain returns a new instance of Main.
func NewMain() *Main {
	// Create a temporary data directory.
	dataDir, err := ioutil.TempDir("", "flynn-redis-")
	if err != nil {
		panic(err)
	}

	// Create test wrapper with random port and temporary data directory.
	m := &Main{
		Main:            main.NewMain(),
		DiscoverdClient: NewDiscoverdClient(),
	}
	m.Main.Addr = "127.0.0.1:0"
	m.Main.DataDir = dataDir
	m.Main.DiscoverdClient = m.DiscoverdClient

	m.Main.Stdin = &m.Stdin
	m.Main.Stdout = &m.Stdout
	m.Main.Stderr = &m.Stderr

	if testing.Verbose() {
		m.Main.Stdout = io.MultiWriter(os.Stdout, m.Main.Stdout)
		m.Main.Stderr = io.MultiWriter(os.Stderr, m.Main.Stderr)
	}

	return m
}

// Close cleans up temporary paths and closes the program.
func (m *Main) Close() error {
	defer os.RemoveAll(m.DataDir)
	return m.Main.Close()
}

// URL returns the string URL for communicating with the program's HTTP handler.
func (m *Main) URL() string { return "http://" + m.Listener().Addr().String() }

// DiscoverdClient is a mock implementation of main.Main.DiscoverdClient.
type DiscoverdClient struct {
	AddServiceFn       func(name string, config *discoverd.ServiceConfig) error
	RegisterInstanceFn func(service string, inst *discoverd.Instance) (discoverd.Heartbeater, error)
}

// NewDiscoverdClient returns a new instance of DiscoverdClient with default mock implementations.
func NewDiscoverdClient() *DiscoverdClient {
	return &DiscoverdClient{
		AddServiceFn: func(name string, config *discoverd.ServiceConfig) error { return nil },
		RegisterInstanceFn: func(service string, inst *discoverd.Instance) (discoverd.Heartbeater, error) {
			return NewHeartbeater(inst.Addr), nil
		},
	}
}

func (c *DiscoverdClient) AddService(name string, config *discoverd.ServiceConfig) error {
	return c.AddServiceFn(name, config)
}
func (c *DiscoverdClient) RegisterInstance(service string, inst *discoverd.Instance) (discoverd.Heartbeater, error) {
	return c.RegisterInstanceFn(service, inst)
}

// Heartbeater is a mock implementation of discoverd.Heartbeater.
type Heartbeater struct {
	SetMetaFn   func(map[string]string) error
	CloseFn     func() error
	AddrFn      func() string
	SetClientFn func(*discoverd.Client)
}

// NewHeartbeater returns a new instance of Heartbeater with default mock implementations.
func NewHeartbeater(addr string) *Heartbeater {
	return &Heartbeater{
		SetMetaFn:   func(map[string]string) error { return nil },
		CloseFn:     func() error { return nil },
		AddrFn:      func() string { return addr },
		SetClientFn: func(*discoverd.Client) {},
	}
}

func (h *Heartbeater) SetMeta(m map[string]string) error { return h.SetMetaFn(m) }
func (h *Heartbeater) Close() error                      { return h.CloseFn() }
func (h *Heartbeater) Addr() string                      { return h.AddrFn() }
func (h *Heartbeater) SetClient(c *discoverd.Client)     { h.SetClientFn(c) }
