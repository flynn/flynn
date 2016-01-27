package mariadb_test

import (
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/flynn/flynn/appliance/mariadb"
)

// Ensure process can start and stop successfully.
func TestProcess_Start(t *testing.T) {
	p := NewProcess()
	if err := p.Start(); err != nil {
		t.Fatal(err)
	}
	if err := p.Stop(); err != nil {
		t.Fatal(err)
	}
}

// Ensure process returns an error if already running.
func TestProcess_Start_ErrRunning(t *testing.T) {
	p := NewProcess()
	defer p.Stop()
	if err := p.Start(); err != nil {
		t.Fatal(err)
	} else if err := p.Start(); err != mariadb.ErrRunning {
		t.Fatalf("unexpected error: %s", err)
	}
}

// Ensure process returns an error if already stopped.
func TestProcess_Stop_ErrStopped(t *testing.T) {
	p := NewProcess()
	if err := p.Stop(); err != mariadb.ErrStopped {
		t.Fatalf("unexpected error: %s", err)
	}
}

// Process is a test wrapper for mariadb.Process.
type Process struct {
	*mariadb.Process
	ProcessClient
}

// NewProcess creates a new Process on a random port.
func NewProcess() *Process {
	// Create temporary directory for data.
	path, err := ioutil.TempDir("", "flynn-mariadb-")
	if err != nil {
		panic(err)
	}

	// Create random port.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	// Find parent directory for binary.
	binPath, err := exec.LookPath("mysqld")
	if err != nil {
		panic("mysqld not found in path: " + err.Error())
	}

	// Create process
	p := &Process{Process: mariadb.NewProcess()}
	p.ID = fmt.Sprintf("P%d", atomic.AddUint64(&nextProcessID, 1))
	p.BinDir = filepath.Dir(binPath)
	p.DataDir = path
	p.Port = strconv.Itoa(port)
	p.Password = "flynn"

	// Mock client.
	p.Process.Client = &p.ProcessClient
	p.ProcessClient.PingFn = DefaultPingFunc

	return p
}

// MustStartProcess returns a new, started Process. Panic on error.
func MustStartProcess() *Process {
	p := NewProcess()
	if err := p.Start(); err != nil {
		panic(err)
	}
	return p
}

// Stop stops the process and removes the underlying data directory.
func (p *Process) Stop() error {
	defer os.RemoveAll(p.DataDir)
	return p.Process.Stop()
}

// nextProcessID is used for atomically incrementing the process's ID.
var nextProcessID uint64

// ProcessClient is a mockable implementation of mariadb.ProcessClient.
type ProcessClient struct {
	PingFn func(host string, timeout time.Duration) error
}

func (c *ProcessClient) Ping(host string, timeout time.Duration) error {
	return c.PingFn(host, timeout)
}

// DefaultPingFunc returns a nil error to indicate a successful ping.
func DefaultPingFunc(host string, timeout time.Duration) error { return nil }
