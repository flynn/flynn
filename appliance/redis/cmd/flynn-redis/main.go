package main

import (
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"path/filepath"

	"github.com/flynn/flynn/appliance/redis"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/keepalive"
	"github.com/flynn/flynn/pkg/random"
	"github.com/flynn/flynn/pkg/shutdown"
	"github.com/inconshreveable/log15"
)

const (
	// DefaultServiceName is the service name used if FLYNN_REDIS is empty.
	DefaultServiceName = "redis"

	// DefaultAddr is the default bind address for the HTTP API.
	DefaultAddr = ":6380"

	// DefaultDataDir is the default base directory for data storage.
	DefaultDataDir = "/data"
)

func main() {
	m := NewMain()
	if err := m.ParseFlags(os.Args[1:]); err != nil {
		shutdown.Fatal(err)
	}

	if err := m.Run(); err != nil {
		shutdown.Fatal(err)
	}
	<-(chan struct{})(nil)
}

// Main represent the main program.
type Main struct {
	ln net.Listener
	hb discoverd.Heartbeater

	// Name of the service to register with discoverd.
	ServiceName string

	// Bind address for the HTTP API.
	Addr string

	// Base directory for data storage.
	DataDir string

	Process *redis.Process
	Logger  log15.Logger

	// Client for service and instance registration.
	DiscoverdClient interface {
		AddService(name string, config *discoverd.ServiceConfig) error
		RegisterInstance(service string, inst *discoverd.Instance) (discoverd.Heartbeater, error)
	}

	// Standard input/output
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

// NewMain returns a new instance of Main.
func NewMain() *Main {
	return &Main{
		ServiceName: DefaultServiceName,
		Addr:        DefaultAddr,
		DataDir:     DefaultDataDir,

		Process: redis.NewProcess(),
		Logger:  log15.New("app", DefaultServiceName),

		DiscoverdClient: discoverd.DefaultClient,

		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	}
}

// Listener returns the program's listener.
func (m *Main) Listener() net.Listener { return m.ln }

// ParseFlags parses the command line flags and environment variables.
func (m *Main) ParseFlags(args []string) error {
	// Use service name, if specified.
	if s := os.Getenv("FLYNN_REDIS"); s != "" {
		m.ServiceName = s
		m.Logger = log15.New("app", m.ServiceName)
	}

	// Extract password from environment variable.
	m.Process.Password = os.Getenv("REDIS_PASSWORD")

	return nil
}

// Run executes the program.
func (m *Main) Run() error {
	m.Logger.Info("running")

	// Read or generate the instance identifier from file.
	id, err := m.readID(filepath.Join(m.DataDir, "instance_id"))
	if err != nil {
		return err
	}
	m.Process.ID = id
	m.Process.DataDir = m.DataDir
	m.Process.Logger = m.Logger.New("component", "process", "id", id)

	// Start process.
	m.Logger.Info("starting process", "id", id)
	if err := m.Process.Start(); err != nil {
		m.Logger.Error("error starting process", "err", err)
		return err
	}

	// Add service to discoverd registry.
	m.Logger.Info("adding service", "name", m.ServiceName)
	if err = m.DiscoverdClient.AddService(m.ServiceName, nil); err != nil && !httphelper.IsObjectExistsError(err) {
		m.Logger.Error("error adding discoverd service", "err", err)
		return err
	}
	inst := &discoverd.Instance{
		Addr: ":" + m.Process.Port,
		Meta: map[string]string{"REDIS_ID": id},
	}

	// Register instance and retain heartbeater.
	m.Logger.Info("registering instance", "addr", inst.Addr, "meta", inst.Meta)
	hb, err := m.DiscoverdClient.RegisterInstance(m.ServiceName, inst)
	if err != nil {
		m.Logger.Error("error registering discoverd instance", "err", err)
		return err
	}
	m.hb = hb
	shutdown.BeforeExit(func() { hb.Close() })

	m.Logger.Info("opening port", "addr", m.Addr)

	// Open HTTP port.
	ln, err := net.Listen("tcp", m.Addr)
	if err != nil {
		m.Logger.Error("error opening port", "err", err)
		return err
	}
	m.ln = keepalive.Listener(ln)

	// Initialize and server handler.
	m.Logger.Info("serving http api")
	h := redis.NewHandler()
	h.Process = m.Process
	h.Heartbeater = m.hb
	h.Logger = m.Logger.New("component", "http")
	go func() { http.Serve(ln, h) }()

	return nil
}

// Close cleanly shuts down the program.
func (m *Main) Close() error {
	logger := m.Logger.New("fn", "Close")

	if m.ln != nil {
		if err := m.ln.Close(); err != nil {
			logger.Error("error closing listener", "err", err)
		}
	}

	if m.hb != nil {
		if err := m.hb.Close(); err != nil {
			logger.Error("error stopping heartbeater", "err", err)
		}
	}

	if err := m.Process.Stop(); err != nil {
		logger.Error("error stopping process", "err", err)
	}

	return nil
}

// readID reads the instance id from path.
// If the file instance id doesn't exist then a random ID is generated.
func (m *Main) readID(path string) (string, error) {
	buf, err := ioutil.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("error reading instance ID: %s", err)
	}

	// If the ID exists then return it immediately.
	id := string(buf)
	if id != "" {
		return id, nil
	}

	// Generate a new ID and write it to file.
	id = random.UUID()
	if err := ioutil.WriteFile(path, []byte(id), 0644); err != nil {
		return "", fmt.Errorf("error writing instance ID: %s", err)
	}
	return id, nil
}
