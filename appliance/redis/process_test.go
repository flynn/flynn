package redis_test

import (
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/flynn/flynn/appliance/redis"
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
	} else if err := p.Start(); err != redis.ErrRunning {
		t.Fatalf("unexpected error: %s", err)
	}
}

// Ensure process returns an error if already stopped.
func TestProcess_Stop_ErrStopped(t *testing.T) {
	p := NewProcess()
	if err := p.Stop(); err != redis.ErrStopped {
		t.Fatalf("unexpected error: %s", err)
	}
}

// Ensure process can retrieve server status.
func TestProcess_Info(t *testing.T) {
	p := MustStartProcess()
	defer p.Stop()
	if info, err := p.Info(); err != nil {
		t.Fatal(err)
	} else if !reflect.DeepEqual(info, &redis.ProcessInfo{Running: true}) {
		t.Fatalf("unexpected info: %#v", info)
	}
}

// Ensure process can retrieve internal Redis info.
func TestProcess_RedisInfo(t *testing.T) {
	p := MustStartProcess()
	defer p.Stop()
	if info, err := p.RedisInfo("", 30*time.Second); err != nil {
		t.Fatal(err)
	} else if !reflect.DeepEqual(info, &redis.RedisInfo{Role: "master"}) {
		t.Fatalf("unexpected info: %#v", info)
	}
}

// Ensure the response from an INFO command can be parsed.
func TestParseRedisInfo_Replication(t *testing.T) {
	info, err := redis.ParseRedisInfo(`
# Replication
role:master
master_host:hostA
master_port:1234
master_link_status:up
master_last_io_seconds_ago:12
master_sync_in_progress:1
master_sync_left_bytes:100
master_sync_last_io_seconds_ago:15
master_link_down_since_seconds:20
connected_slaves:3
`)

	if err != nil {
		t.Fatal(err)
	} else if !reflect.DeepEqual(info, &redis.RedisInfo{
		Role:                 "master",
		MasterHost:           "hostA",
		MasterPort:           1234,
		MasterLinkStatus:     "up",
		MasterLastIO:         12 * time.Second,
		MasterSyncInProgress: true,
		MasterSyncLeftBytes:  100,
		MasterSyncLastIO:     15 * time.Second,
		MasterLinkDownSince:  20 * time.Second,
		ConnectedSlaves:      3,
	}) {
		t.Fatalf("unexpected info: %#v", info)
	}
}

// Process is a test wrapper for redis.Process.
type Process struct {
	*redis.Process
}

// NewProcess creates a new Process on a random port.
func NewProcess() *Process {
	// Create temporary directory for data.
	path, err := ioutil.TempDir("", "flynn-redis-")
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

	// Find parent directory for redis.
	binPath, err := exec.LookPath("redis-server")
	if err != nil {
		panic("redis-server not found in path: " + err.Error())
	}

	// Create process
	p := &Process{Process: redis.NewProcess()}
	p.ID = fmt.Sprintf("P%d", atomic.AddUint64(&nextProcessID, 1))
	p.BinDir = filepath.Dir(binPath)
	p.DataDir = path
	p.Port = strconv.Itoa(port)
	p.Password = "flynn"
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
