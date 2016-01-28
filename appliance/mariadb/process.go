package mariadb

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"text/template"
	"time"

	_ "github.com/flynn/flynn/Godeps/_workspace/src/github.com/go-sql-driver/mysql"
	"github.com/flynn/flynn/Godeps/_workspace/src/gopkg.in/inconshreveable/log15.v2"
	"github.com/flynn/flynn/appliance/postgresql/state"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/shutdown"
)

const (
	DefaultPort        = "3306"
	DefaultBinDir      = "/usr/bin"
	DefaultSbinDir     = "/usr/sbin"
	DefaultDataDir     = "/data"
	DefaultPassword    = ""
	DefaultServerID    = 1
	DefaultOpTimeout   = 5 * time.Minute
	DefaultReplTimeout = 1 * time.Minute

	BinName    = "mysqld"
	ConfigName = "my.cnf"

	checkInterval = 100 * time.Millisecond
)

var (
	// ErrRunning is returned when starting an already running process.
	ErrRunning = errors.New("process already running")

	// ErrStopped is returned when stopping an already stopped process.
	ErrStopped = errors.New("process already stopped")
)

// Process represents a MariaDB process.
type Process struct {
	mtx sync.Mutex

	// Replication configuration
	configValue   atomic.Value // *Config
	configApplied bool

	runningValue          atomic.Value // bool
	syncedDownstreamValue atomic.Value // *discoverd.Instance

	ID           string
	Singleton    bool
	Port         string
	BinDir       string
	SbinDir      string
	DataDir      string
	Password     string
	ServerID     int
	OpTimeout    time.Duration
	ReplTimeout  time.Duration
	WaitUpstream bool

	Logger log15.Logger

	// cmd is the running system command.
	cmd *Cmd

	// cancelSyncWait cancels the goroutine that is waiting for
	// the downstream to catch up, if running.
	cancelSyncWait func()
}

// NewProcess returns a new instance of Process.
func NewProcess() *Process {
	p := &Process{
		Port:        DefaultPort,
		BinDir:      DefaultBinDir,
		SbinDir:     DefaultSbinDir,
		DataDir:     DefaultDataDir,
		Password:    DefaultPassword,
		OpTimeout:   DefaultOpTimeout,
		ReplTimeout: DefaultReplTimeout,
		Logger:      log15.New("app", "mariadb"),

		cancelSyncWait: func() {},
	}
	p.runningValue.Store(false)
	p.configValue.Store((*state.PgConfig)(nil))
	return p
}

func (p *Process) running() bool           { return p.runningValue.Load().(bool) }
func (p *Process) config() *state.PgConfig { return p.configValue.Load().(*state.PgConfig) }

func (p *Process) syncedDownstream() *discoverd.Instance {
	return p.syncedDownstreamValue.Load().(*discoverd.Instance)
}

func (p *Process) ConfigPath() string { return filepath.Join(p.DataDir, "my.cnf") }

func (p *Process) Reconfigure(config *state.PgConfig) error {
	p.mtx.Lock()
	defer p.mtx.Unlock()

	switch config.Role {
	case state.RolePrimary:
		if !p.Singleton && config.Downstream == nil {
			return errors.New("missing downstream peer")
		}
	case state.RoleSync, state.RoleAsync:
		if config.Upstream == nil {
			return fmt.Errorf("missing upstream peer")
		}
	case state.RoleNone:
	default:
		return fmt.Errorf("unknown role %v", config.Role)
	}

	if !p.running() {
		p.configValue.Store(config)
		p.configApplied = false
		return nil
	}

	return p.reconfigure(config)
}

func (p *Process) Start() error {
	p.mtx.Lock()
	defer p.mtx.Unlock()

	if p.running() {
		return errors.New("process already running")
	}
	if p.config() == nil {
		return errors.New("unconfigured process")
	}
	if p.config().Role == state.RoleNone {
		return errors.New("start attempted with role 'none'")
	}

	return p.reconfigure(nil)
}

func (p *Process) Stop() error {
	p.mtx.Lock()
	defer p.mtx.Unlock()

	if !p.running() {
		return errors.New("process already stopped")
	}
	return p.stop()
}

func (p *Process) reconfigure(config *state.PgConfig) error {
	logger := p.Logger.New("fn", "reconfigure")

	if err := func() error {
		if config != nil && config.Role == state.RoleNone {
			logger.Info("nothing to do", "reason", "null role")
			return nil
		}

		// If we've already applied the same config, we don't need to do anything
		if p.configApplied && config != nil && p.config() != nil && config.Equal(p.config()) {
			logger.Info("nothing to do", "reason", "config already applied")
			return nil
		}

		// If we're already running and it's just a change from async to sync with the same node, we don't need to restart
		if p.configApplied && p.running() && p.config() != nil && config != nil &&
			p.config().Role == state.RoleAsync && config.Role == state.RoleSync && config.Upstream.Meta["MYSQL_ID"] == p.config().Upstream.Meta["MYSQL_ID"] {
			logger.Info("nothing to do", "reason", "becoming sync with same upstream")
			return nil
		}

		// Make sure that we don't keep waiting for replication sync while reconfiguring
		p.cancelSyncWait()
		p.syncedDownstreamValue.Store((*discoverd.Instance)(nil))

		// If we're already running and this is only a downstream change, just wait for the new downstream to catch up
		if p.running() && p.config().IsNewDownstream(config) {
			logger.Info("downstream changed", "to", config.Downstream.Addr)
			p.waitForSync(config.Downstream, false)
			return nil
		}

		if config == nil {
			config = p.config()
		}

		if config.Role == state.RolePrimary {
			return p.assumePrimary(config.Downstream)
		}

		return p.assumeStandby(config.Upstream, config.Downstream)
	}(); err != nil {
		return err
	}

	// Apply configuration.
	p.configValue.Store(config)
	p.configApplied = true

	return nil
}

func (p *Process) assumePrimary(downstream *discoverd.Instance) (err error) {
	logger := p.Logger.New("fn", "assumePrimary")
	if downstream != nil {
		logger = logger.New("downstream", downstream.Addr)
	}

	logger.Info("starting as primary")

	// Assert that the process is not running. This should not occur.
	if p.running() {
		panic(fmt.Sprintf("unexpected state running role=%s", p.config().Role))
	}

	if err := p.writeConfig(configData{ReadOnly: downstream != nil}); err != nil {
		logger.Error("error writing config", "path", p.ConfigPath(), "err", err)
		return err
	}

	if err := p.installDB(); err != nil {
		return err
	}

	if err := p.start(); err != nil {
		return err
	}

	if err := func() error {
		dsn := p.DSN("127.0.0.1:"+p.Port, "")
		dsn.User = "root"
		dsn.Password = ""

		db, err := sql.Open("mysql", dsn.String())
		if err != nil {
			logger.Error("error acquiring connection", "err", err)
			return err
		}
		defer db.Close()

		tx, err := db.Begin()
		if err != nil {
			logger.Error("error starting transaction", "err", err)
			return err
		}
		defer tx.Rollback()

		if _, err := tx.Exec(fmt.Sprintf(`CREATE USER 'flynn'@'%%' IDENTIFIED BY '%s'`, p.Password)); err != nil {
			return err
		}
		if _, err := tx.Exec(`GRANT ALL PRIVILEGES ON *.* TO 'flynn'@'%'`); err != nil {
			return err
		}
		if _, err := tx.Exec(`GRANT REPLICATION SLAVE ON *.* TO 'flynn'@'%'`); err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			logger.Error("error committing transaction", "err", err)
			return err
		}

		if _, err := db.Exec(`FLUSH PRIVILEGES`); err != nil {
			return err
		}

		if downstream != nil {
			p.waitForSync(downstream, true)
		}

		return nil
	}(); err != nil {
		if e := p.stop(); err != nil {
			logger.Debug("ignoring error stopping process", "err", e)
		}
		return err
	}

	return nil
}

func (p *Process) assumeStandby(upstream, downstream *discoverd.Instance) error {
	logger := p.Logger.New("fn", "assumeStandby", "upstream", upstream.Addr)
	logger.Info("starting up as standby")

	if p.running() {
		if err := p.stop(); err != nil {
			return err
		}
	} else {
		if p.WaitUpstream {
			if err := p.waitForUpstream(upstream); err != nil {
				return err
			}
		}

		// FIXME: retrieve backup
		/*
			logger.Info("retrieving backup")

			err := p.runCmd(exec.Command(
				filepath.Join(p.BinDir, "pg_basebackup"),
				"--pgdata", p.DataDir,
				"--dbname", fmt.Sprintf(
					"host=%s port=%s user=flynn password=%s application_name=%s",
					upstream.Host(), upstream.Port(), p.Password, upstream.Meta["MYSQL_ID"],
				),
				"--xlog-method=stream",
				"--progress",
				"--verbose",
			))
			if err != nil {
				logger.Error("error pulling basebackup", "err", err)
				if files, err := ioutil.ReadDir("/data"); err == nil {
					for _, file := range files {
						os.RemoveAll(filepath.Join("/data", file.Name()))
					}
				}
				return err
			}
		*/
	}

	if err := p.writeConfig(configData{ReadOnly: true}); err != nil {
		logger.Error("error writing config", "path", p.ConfigPath(), "err", err)
		return err
	}

	if err := p.start(); err != nil {
		return err
	}

	// FIXME: Apply backup.

	if downstream != nil {
		p.waitForSync(downstream, false)
	}

	return nil
}

// upstreamTimeout is of the order of the discoverd heartbeat to prevent
// waiting for an upstream which has gone down.
var upstreamTimeout = 10 * time.Second

func (p *Process) waitForUpstream(upstream *discoverd.Instance) error {
	logger := p.Logger.New("fn", "waitForUpstream", "upstream", upstream.Addr)
	logger.Info("waiting for upstream to come online")
	client := NewClient(upstream.Addr)

	start := time.Now()
	for {
		status, err := client.Status()
		if err != nil {
			logger.Error("error getting upstream status", "err", err)
		} else if status.Process.Running { // FIXME: && status.Process.XLog != "" && status.Process.UserExists
			logger.Info("upstream is online")
			return nil
		}
		time.Sleep(checkInterval)
		if time.Now().Sub(start) > upstreamTimeout {
			logger.Error("upstream did not come online in time")
			return errors.New("upstream is offline")
		}
	}
}

func (p *Process) start() error {
	logger := p.Logger.New("fn", "start", "data_dir", p.DataDir, "sbin_dir", p.SbinDir)
	logger.Info("starting process")

	cmd := NewCmd(exec.Command(filepath.Join(p.SbinDir, "mysqld"), "--defaults-extra-file="+p.ConfigPath()))
	if err := cmd.Start(); err != nil {
		logger.Error("failed to start process", "err", err)
		return err
	}
	p.cmd = cmd
	p.runningValue.Store(true)

	go func() {
		if <-cmd.Stopped(); cmd.Err() != nil {
			logger.Error("process unexpectedly exit", "err", cmd.Err())
			shutdown.ExitWithCode(1)
		}
	}()

	logger.Debug("waiting for process to start")

	timer := time.NewTimer(p.OpTimeout)
	defer timer.Stop()

	for {
		// Connect to server.
		// Retry after sleep if an error occurs.
		if err := func() error {
			dsn := p.DSN("127.0.0.1:"+p.Port, "")
			dsn.User = "root"
			dsn.Password = ""

			db, err := sql.Open("mysql", dsn.String())
			if err != nil {
				return err
			}
			defer db.Close()

			if _, err := db.Exec("SELECT 1"); err != nil {
				return err
			}

			return nil
		}(); err != nil {
			select {
			case <-timer.C:
				logger.Error("timed out waiting for process to start", "err", err)
				if err := p.stop(); err != nil {
					logger.Error("error stopping process", "err", err)
				}
				return err
			default:
				logger.Debug("ignoring error connecting to mysql", "err", err)
				time.Sleep(checkInterval)
				continue
			}
		}

		return nil
	}
}

func (p *Process) stop() error {
	logger := p.Logger.New("fn", "stop")
	logger.Info("stopping mysql")

	p.cancelSyncWait()

	// Attempt to kill.
	logger.Debug("stopping daemon")
	if err := p.cmd.Stop(); err != nil {
		logger.Error("error stopping command", "err", err)
	}

	// Wait for cmd to stop or timeout.
	select {
	case <-time.After(p.OpTimeout):
		return errors.New("unable to kill process")
	case <-p.cmd.Stopped():
		p.runningValue.Store(false)
		return nil
	}
}

func (p *Process) Info() (*ProcessInfo, error) {
	return &ProcessInfo{
		Config:           p.config(),
		Running:          p.running(),
		SyncedDownstream: p.syncedDownstream(),
	}, nil
}

func (p *Process) waitForSync(downstream *discoverd.Instance, enableWrites bool) {
	stopCh := make(chan struct{})
	doneCh := make(chan struct{})

	var once sync.Once
	p.cancelSyncWait = func() {
		once.Do(func() { close(stopCh); <-doneCh })
	}

	go func() {
		defer close(doneCh)

		var status SlaveStatus
		startTime := time.Now().UTC()
		logger := p.Logger.New(
			"fn", "waitForSync",
			"sync_name", downstream.Meta["MYSQL_ID"],
			"start_time", log15.Lazy{func() time.Time { return startTime }},
			"log_file", log15.Lazy{func() string { return status.MasterLogFile }},
			"log_pos", log15.Lazy{func() int64 { return status.ReadMasterLogPos }},
		)

		sleep := func() bool {
			select {
			case <-stopCh:
				logger.Debug("canceled, stopping")
				return false
			case <-time.After(checkInterval):
				return true
			}
		}

		logger.Info("waiting for downstream replication to catch up")

		for {
			// Check if "wait sync" has been canceled.
			select {
			case <-stopCh:
				logger.Debug("canceled, stopping")
				return
			default:
			}

			// Read local master status.
			ms, err := p.masterStatus("127.0.0.1")
			if err != nil {
				startTime = time.Now().UTC()
				if !sleep() {
					return
				}
				continue
			}

			// Read downstream slave status.
			ss, err := p.slaveStatus(downstream.Meta["MYSQL_ID"])
			if err != nil {
				startTime = time.Now().UTC()
				if !sleep() {
					return
				}
				continue
			}

			elapsedTime := time.Since(startTime)
			logger := logger.New(
				"master_log_file", ms.File, "master_log_pos", ms.Position,
				"slave_log_file", ss.MasterLogFile, "slave_log_pos", ss.ReadMasterLogPos,
				"elapsed", elapsedTime,
			)

			// Update the slave status and reset start time if replication has progressed.
			if cmp := ComparePosition(status.MasterLogFile, status.ReadMasterLogPos, ss.MasterLogFile, ss.ReadMasterLogPos); status.IsZero() || cmp == -1 {
				logger.Debug("slave status progressing, resetting start time")
				startTime = time.Now().UTC()
				status = ss
			}

			if ms.File == ss.MasterLogFile && ms.Position == ss.ReadMasterLogPos {
				logger.Info("downstream caught up")
				p.syncedDownstreamValue.Store(downstream)
				break
			}

			if elapsedTime > p.ReplTimeout {
				logger.Error("error checking replication status", "err", "downstream unable to make forward progress")
				return
			}

			logger.Debug("continuing replication check")
			if !sleep() {
				return
			}
		}

		if enableWrites {
			panic("FIXME: enable writes, restart?")
		}
	}()
}

// DSN returns the datasource name for a host.
func (p *Process) DSN(host, database string) *DSN {
	return &DSN{
		Host:     host,
		Database: database,
		User:     "flynn",
		Password: p.Password,
		Timeout:  p.OpTimeout,
	}
}

var ErrNoReplicationStatus = errors.New("no replication status")

func (p *Process) masterStatus(host string) (status MasterStatus, err error) {
	db, err := sql.Open("mysql", p.DSN(host, "").String())
	if err != nil {
		return
	}
	defer db.Close()

	if err = db.QueryRow("SHOW MASTER STATUS").Scan(&status.File, &status.Position); err != nil {
		return
	}
	return
}

// XLogPosition returns the current transaction log position.
func (p *Process) XLogPosition() (Position, error) { panic("FIXME") }

func (p *Process) slaveStatus(host string) (status SlaveStatus, err error) {
	db, err := sql.Open("mysql", p.DSN(host, "").String())
	if err != nil {
		return
	}
	defer db.Close()

	if err = db.QueryRow("SHOW SLAVE STATUS").Scan(
		&status.SlaveIOState, // Slave_IO_State
		nil,                  // Master_Host
		nil,                  // Master_User
		nil,                  // Master_Port
		nil,                  // Connect_Retry
		&status.MasterLogFile,      // Master_Log_File
		&status.ReadMasterLogPos,   // Read_Master_Log_Pos
		&status.RelayLogFile,       // Relay_Log_File
		&status.RelayLogPos,        // Relay_Log_Pos
		&status.RelayMasterLogFile, // Relay_Master_Log_File
		&status.SlaveIORunning,     // Slave_IO_Running
		nil, // Slave_SQL_Running
		nil, // Replicate_Do_DB
		nil, // Replicate_Ignore_DB
		nil, // Replicate_Do_Table
		nil, // Replicate_Ignore_Table
		nil, // Replicate_Wild_Do_Table
		nil, // Replicate_Wild_Ignore_Table
		nil, // Last_Errno
		nil, // Last_Error
		nil, // Skip_Counter
		&status.ExecMasterLogPos, // Exec_Master_Log_Pos
		nil, // Relay_Log_Space
		nil, // Until_Condition
		nil, // Until_Log_File
		nil, // Until_Log_Pos
		nil, // Master_SSL_Allowed
		nil, // Master_SSL_CA_File
		nil, // Master_SSL_CA_Path
		nil, // Master_SSL_Cert
		nil, // Master_SSL_Cipher
		nil, // Master_SSL_Key
		&status.SecondsBehindMaster, // Seconds_Behind_Master
		nil,                 // Master_SSL_Verify_Server_Cert
		&status.LastIOErrno, // Last_IO_Errno
		&status.LastIOError, // Last_IO_Error
		nil,                 // Last_SQL_Errno
		nil,                 // Last_SQL_Error
		nil,                 // Replicate_Ignore_Server_Ids
	); err != nil {
		return
	}
	return
}

// installDB initializes the data directory for a new database.
func (p *Process) installDB() error {
	logger := p.Logger.New("fn", "installDB", "data_dir", p.DataDir)
	logger.Debug("starting installDB")

	// Ignore errors, since the db could be already initialized
	p.runCmd(exec.Command(
		filepath.Join(p.BinDir, "mysql_install_db"),
		"--defaults-extra-file="+p.ConfigPath(),
	))

	return nil
}
func (p *Process) runCmd(cmd *exec.Cmd) error {
	p.Logger.Debug("running command", "fn", "runCmd", "cmd", cmd.Args)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (p *Process) writeConfig(d configData) error {
	d.ID = p.ID
	d.Port = p.Port
	d.DataDir = p.DataDir
	d.ServerID = 1 // FIXME: Determine from process.

	f, err := os.Create(p.ConfigPath())
	if err != nil {
		return err
	}
	defer f.Close()

	return configTemplate.Execute(f, d)
}

type configData struct {
	ID       string
	Port     string
	Sync     string
	DataDir  string
	ServerID int
	ReadOnly bool
}

var configTemplate = template.Must(template.New("my.cnf").Parse(`
[client]
port = {{.Port}}

[mysqld]
user         = ""
port         = {{.Port}}
bind-address = 0.0.0.0
server-id    = {{.ServerID}}
socket       = ""
pid-file     = {{.DataDir}}/mysql.pid

datadir             = {{.DataDir}}
log-bin             = {{.DataDir}}/mariadb-bin
log-bin-index       = {{.DataDir}}/mariadb-bin.index
wsrep-data-home-dir = {{.DataDir}}
`[1:]))

type ProcessInfo struct {
	Config           *state.PgConfig     `json:"config"`
	Running          bool                `json:"running"`
	SyncedDownstream *discoverd.Instance `json:"synced_downstream"`
}

// Cmd wraps exec.Cmd and provides helpers for checking for expected exits.
type Cmd struct {
	*exec.Cmd
	stoppingValue atomic.Value
	stopped       chan struct{}
	err           error
}

// NewCmd returns a new instance of Cmd that wraps cmd.
func NewCmd(cmd *exec.Cmd) *Cmd {
	c := &Cmd{
		Cmd:     cmd,
		stopped: make(chan struct{}, 1),
	}
	c.stoppingValue.Store(false)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c
}

// Start executes the command.
func (cmd *Cmd) Start() error {
	if err := cmd.Cmd.Start(); err != nil {
		return err
	}
	go cmd.monitor()
	return nil
}

// Stop marks the command as expecting an exit and stops the underlying command.
func (cmd *Cmd) Stop() error {
	cmd.stoppingValue.Store(true)
	if err := cmd.Process.Signal(syscall.SIGKILL); err != nil {
		return err
	}
	return nil
}

// Stopped returns a channel that returns an error if stopped unsuccessfully.
func (cmd *Cmd) Stopped() <-chan struct{} { return cmd.stopped }

// Err returns an error if cmd stopped unexpectedly.
// Must wait for the Stopped channel to return first.
func (cmd *Cmd) Err() error { return cmd.err }

// monitor checks for process exit and returns
func (cmd *Cmd) monitor() {
	err := cmd.Wait()
	if !cmd.stoppingValue.Load().(bool) {
		cmd.err = err
	}
	close(cmd.stopped)
}

// MasterStatus represents the data returned from a "SHOW MASTER STATUS" command.
type MasterStatus struct {
	File     string `json:"file"`
	Position int64  `json:"position"`
}

// SlaveStatus represents the data returned from a "SHOW SLAVE STATUS" command.
type SlaveStatus struct {
	// The current status of the slave.
	SlaveIOState string

	// Whether the I/O thread for reading the master's binary log is running.
	SlaveIORunning bool

	// The last errors registered by the I/O thread when processing the relay log.
	LastIOErrno int64
	LastIOError string

	// Coordinates in the master binary log indicating how far the slave I/O
	// thread has read events from that log.
	MasterLogFile    string
	ReadMasterLogPos int64

	// Coordinates in the master binary log indicating how far the slave SQL
	// thread has executed events received from that log.
	RelayMasterLogFile string
	ExecMasterLogPos   int64

	// Coordinates in the slave relay log indicating how far the slave SQL
	// thread has executed the relay log.
	RelayLogFile string
	RelayLogPos  int64

	// The number of seconds that the slave SQL thread is behind processing the
	// master binary log.
	SecondsBehindMaster int64
}

// IsZero returns true if s is the zero value.
func (s *SlaveStatus) IsZero() bool { return *s == (SlaveStatus{}) }

// ComparePosition compares two binlog positions.
func ComparePosition(file1 string, pos1 int64, file2 string, pos2 int64) int {
	if file1 == file1 && pos1 == pos2 {
		return 0
	} else if file1 > file2 || file1 == file2 && pos1 > pos2 {
		return 1
	}
	return -1
}

// DSN returns a URL-formatted data source name.
type DSN struct {
	Host     string
	User     string
	Password string
	Database string
	Timeout  time.Duration
}

// String encodes dsn to a URL string format.
func (dsn *DSN) String() string {
	u := url.URL{
		Host: fmt.Sprintf("tcp(%s)", dsn.Host),
		Path: "/" + dsn.Database,
		RawQuery: url.Values{
			"timeout": {dsn.Timeout.String()},
		}.Encode(),
	}

	// Set password, if available.
	if dsn.Password == "" {
		u.User = url.User(dsn.User)
	} else {
		u.User = url.UserPassword(dsn.User, dsn.Password)
	}

	// Remove leading double-slash.
	return strings.TrimPrefix(u.String(), "//")
}

type Position string

// XLog implements a string serializable, comparable transaction log position.
type XLog struct{}

// Zero Returns the zero position for this xlog
func (xlog *XLog) Zero() Position { return "master-bin.000000/0" }

// Increment increments an xlog position by the given number.
// Returns the new position of the xlog.
func (xlog *XLog) Increment(Position, int) (Position, error) { panic("FIXME") }

// Compare compares two xlog positions returning -1 if xlog1 < xlog2,
// 0 if xlog1 == xlog2, and 1 if xlog1 > xlog2.
func (xlog *XLog) Compare(xlog1, xlog2 Position) (int, error) {
	file1, pos1, err := parseXlog(xlog1)
	if err != nil {
		return 0, err
	}
	file2, pos2, err := parseXlog(xlog2)
	if err != nil {
		return 0, err
	}

	if file1 == file2 && pos1 == pos2 {
		return 0, nil
	}
	if file1 > file2 || file1 == file2 && pos1 > pos2 {
		return 1, nil
	}
	return -1, nil
}

// parseXlog parses a string xlog position into its file and position parts.
// Returns an error if the xlog position is not formatted correctly.
func parseXlog(xlog Position) (file string, pos int, err error) {
	parts := strings.SplitN(string(xlog), "/", 2)
	if len(parts) != 2 {
		err = fmt.Errorf("malformed xlog position %q", xlog)
		return
	}

	file = parts[0]
	pos, err = strconv.Atoi(parts[1])
	if err != nil {
		return "", 0, err
	}

	return file, pos, err
}
