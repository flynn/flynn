package mariadb

import (
	"bytes"
	"database/sql"
	"errors"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"text/template"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/gopkg.in/inconshreveable/log15.v2"
	"github.com/flynn/flynn/pkg/shutdown"
	_ "github.com/go-sql-driver/mysql"
)

const (
	DefaultPort        = "3306"
	DefaultBinDir      = "/usr/sbin"
	DefaultDataDir     = "/data"
	DefaultPassword    = ""
	DefaultBindAddr    = "127.0.0.1"
	DefaultServerID    = 1
	DefaultOpTimeout   = 5 * time.Minute
	DefaultReplTimeout = 1 * time.Minute

	BinName    = "mysqld"
	ConfigName = "my.cnf"

	checkInterval = 100 * time.Millisecond
)

var (
	// ErrTimeout is returned when an operation times out.
	ErrTimeout = errors.New("timeout")

	// ErrRunning is returned when starting an already running process.
	ErrRunning = errors.New("process already running")

	// ErrStopped is returned when stopping an already stopped process.
	ErrStopped = errors.New("process already stopped")
)

// Process represents a running MariaDB process.
type Process struct {
	mtx     sync.Mutex
	running atomic.Value

	stopping atomic.Value
	stopped  chan struct{}

	cmd *exec.Cmd

	ID           string
	Singleton    bool
	Port         string
	BinDir       string
	DataDir      string
	Password     string
	BindAddr     string
	ServerID     int
	OpTimeout    time.Duration
	ReplTimeout  time.Duration
	Logger       log15.Logger
	WaitUpstream bool

	// Client for communicating with local process and other processes.
	Client ProcessClient
}

// NewProcess returns a new instance of Process with defaults.
func NewProcess() *Process {
	p := &Process{
		Port:        DefaultPort,
		BinDir:      DefaultBinDir,
		DataDir:     DefaultDataDir,
		Password:    DefaultPassword,
		OpTimeout:   DefaultOpTimeout,
		ReplTimeout: DefaultReplTimeout,
		Logger:      log15.New("app", "mariadb"),
	}
	p.running.Store(false)
	p.stopping.Store(false)
	return p
}

// ConfigPath returns the path to the configuration file.
func (p *Process) ConfigPath() string { return filepath.Join(p.DataDir, ConfigName) }

// Running returns true if the process is currently running.
func (p *Process) Running() bool { return p.running.Load().(bool) }

// Start begins the process.
// Returns an error if the process is already running.
func (p *Process) Start() error {
	p.mtx.Lock()
	defer p.mtx.Unlock()

	// Valdiate that process is not already running and that we have a config.
	if p.running.Load().(bool) {
		return ErrRunning
	}
	return p.start()
}

func (p *Process) start() error {
	logger := p.Logger.New("fn", "start", "data_dir", p.DataDir, "bin_dir", p.BinDir)

	// Initialize stop state so we know if it's an expected stop.
	p.stopping.Store(false)
	p.stopped = make(chan struct{})

	// Generate configuration.
	if err := p.writeConfig(); err != nil {
		logger.Error("error writing conf", "path", p.ConfigPath(), "err", err)
		return err
	}

	logger.Info("starting process")

	// Execute binary.
	cmd := exec.Command(filepath.Join(p.BinDir, BinName), p.ConfigPath())
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		logger.Error("failed to start", "err", err)
		return err
	}
	p.cmd = cmd
	p.running.Store(true)

	logger.Info("process started")

	// Monitor process to check for unexpected quit.
	go p.monitorCmd(p.cmd, p.stopped)

	// Ping until successful response.
	if err := p.pingWait("localhost", p.OpTimeout); err != nil {
		return err
	}

	return nil
}

// Stop attempts to gracefully stop the process. If the process cannot be
// stopped gracefully then it is forcefully stopped. Returns an error if the
// process is already stopped.
func (p *Process) Stop() error {
	p.mtx.Lock()
	defer p.mtx.Unlock()

	if !p.running.Load().(bool) {
		return ErrStopped
	}
	return p.stop()
}

func (p *Process) stop() error {
	logger := p.Logger.New("fn", "stop")
	logger.Info("stopping")

	// Mark process as expecting a shutdown.
	p.stopping.Store(true)

	// Attempt to kill via escalating signals.
	logger.Debug("signalling daemon")
	if err := p.cmd.Process.Signal(syscall.SIGKILL); err != nil {
		logger.Error("error signalling daemon", "err", err)
	}

	select {
	case <-time.After(p.OpTimeout):
		return errors.New("unable to kill process")
	case <-p.stopped:
		p.running.Store(false)
		return nil
	}
}

// monitorCmd waits for cmd to finish and reports an error if it was unexpected.
// Also closes the stopped channel to notify other listeners that it has finished.
func (p *Process) monitorCmd(cmd *exec.Cmd, stopped chan struct{}) {
	err := cmd.Wait()
	if !p.stopping.Load().(bool) {
		p.Logger.Error("unexpectedly exit", "err", err)
		shutdown.ExitWithCode(1)
	}
	close(stopped)
}

// writeConfig generates a new config file at the config path.
func (p *Process) writeConfig() error {
	logger := p.Logger.New("fn", "writeConfig")
	logger.Info("writing")

	// Generate configuration.
	config := p.config()

	// Create parent directory if it doesn't exist.
	if err := os.MkdirAll(filepath.Dir(p.ConfigPath()), 0777); err != nil {
		logger.Error("cannot create config parent directory", "err", err)
		return err
	}

	// Write configuration file.
	if err := ioutil.WriteFile(p.ConfigPath(), []byte(config), 0666); err != nil {
		logger.Error("cannot create config file", "err", err)
		return err
	}

	return nil
}

// config returns the generated configuration file.
func (p *Process) config() string {
	var buf bytes.Buffer
	if err := configTemplate.Execute(&buf, p); err != nil {
		panic(err)
	}

	return buf.String()
}

// pingWait continually pings a process until it receives a successful response or a timeout occurs.
func (p *Process) pingWait(host string, timeout time.Duration) error {
	logger := p.Logger.New("fn", "ping", "host", host, "timeout", timeout)
	logger.Info("pinging")

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()

	for {
		// Attempt to ping the server.
		if err := p.Client.Ping(host, timeout); err != nil {
			logger.Error("ping error", "err", err)
		} else {
			return nil
		}

		select {
		case <-timer.C:
			logger.Info("ping timeout", "timeout", timeout)
			return ErrTimeout
		case <-ticker.C:
		}
	}
}

// Restore restores process to a backup streamed from r.
func (p *Process) Restore(r io.Reader) error { panic("FIXME") }

// Backup writes a backup from the process to w.
func (p *Process) Backup(w io.Writer) error { panic("FIXME") }

var configTemplate = template.Must(template.New(ConfigName).Parse(`
[client]
port = {{.Port}}

[mysqld]
user    = mysql
port    = {{.Port}}
datadir = {{.DataDir}}

bind-address = {{.BindAddr}}
server-id = {{.ServerID}}
`[1:]))

// ProcessClient represents the interface for communicating with Processes.
type ProcessClient interface {
	// Checks the status of a process. Returns nil if the process is up.
	Ping(host string, timeout time.Duration) error
}

// processClient represents a client for a mysql process.
type processClient struct{}

// NewProcessClient returns a new instance of ProcessClient.
func NewProcessClient() ProcessClient {
	return &processClient{}
}

// Ping returns nil if mysql is running and up on host.
// Returns an error if mysql is not running or available.
func (c *processClient) Ping(host string, timeout time.Duration) error {

}
