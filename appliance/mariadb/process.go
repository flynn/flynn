package mariadb

import (
	"bytes"
	"crypto/sha512"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"text/template"
	"time"

	"github.com/flynn/flynn/appliance/mariadb/mdbxlog"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/shutdown"
	"github.com/flynn/flynn/pkg/sirenia/client"
	"github.com/flynn/flynn/pkg/sirenia/state"
	"github.com/flynn/flynn/pkg/sirenia/xlog"
	"github.com/go-sql-driver/mysql"
	"gopkg.in/inconshreveable/log15.v2"
)

const (
	DefaultPort        = "3306"
	DefaultBinDir      = "/usr/bin"
	DefaultSbinDir     = "/usr/sbin"
	DefaultDataDir     = "/data"
	DefaultPassword    = ""
	DefaultOpTimeout   = 5 * time.Minute
	DefaultReplTimeout = 1 * time.Minute

	BinName    = "mysqld"
	ConfigName = "my.cnf"

	checkInterval = 1000 * time.Millisecond
)

var (
	// ErrRunning is returned when starting an already running process.
	ErrRunning = errors.New("process already running")

	// ErrStopped is returned when stopping an already stopped process.
	ErrStopped = errors.New("process already stopped")

	ErrNoReplicationStatus = errors.New("no replication status")
)

// Process represents a MariaDB process.
type Process struct {
	mtx sync.Mutex

	events chan state.DatabaseEvent

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
	ServerID     uint32
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

		events:         make(chan state.DatabaseEvent, 1),
		cancelSyncWait: func() {},
	}
	p.runningValue.Store(false)
	p.configValue.Store((*state.Config)(nil))
	p.events <- state.DatabaseEvent{}
	return p
}

func (p *Process) running() bool         { return p.runningValue.Load().(bool) }
func (p *Process) config() *state.Config { return p.configValue.Load().(*state.Config) }

func (p *Process) syncedDownstream() *discoverd.Instance {
	if downstream, ok := p.syncedDownstreamValue.Load().(*discoverd.Instance); ok {
		return downstream
	}
	return nil
}

func (p *Process) ConfigPath() string { return filepath.Join(p.DataDir, "my.cnf") }

func (p *Process) Reconfigure(config *state.Config) error {
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

func (p *Process) Ready() <-chan state.DatabaseEvent {
	return p.events
}

func (p *Process) DefaultTunables() state.Tunables {
	return state.Tunables{
		Version: 1,
		Data: map[string]string{
			// Disable the InnoDB double write buffer by default on ZFS as ZFS doesn't allow partial writes
			// and as such the doublewrite buffer serves no purpose.
			"innodb_doublewrite": "0",
			"skip_name_resolve":  "1",
			"query_cache_limit":  "1048576",
		},
	}
}

func (p *Process) ValidateTunables(tunables state.Tunables) error {
	for k := range tunables.Data {
		if _, ok := allowedTunables[k]; !ok {
			return fmt.Errorf("unknown tunable: %s", k)
		}
	}
	return nil
}

func (p *Process) tunablesRequireRestart(config *state.Config) (bool, map[string]struct{}, map[string]struct{}) {
	curTunables := p.config().Tunables.Data
	newTunables := config.Tunables.Data
	changed := make(map[string]struct{})
	removed := make(map[string]struct{})

	// iterate over current to find any changed or removed variables
	for k, v := range curTunables {
		nv, ok := newTunables[k]
		if !ok {
			removed[k] = struct{}{}
		}
		if nv != v {
			changed[k] = struct{}{}
		}
	}

	// then iterate over the new set to find any added variables
	for k := range newTunables {
		if _, ok := curTunables[k]; !ok {
			changed[k] = struct{}{}
		}
	}

	restartRequired := false
	for k := range changed {
		if t := allowedTunables[k]; t.Static {
			restartRequired = true
		}
	}
	for k := range removed {
		if t := allowedTunables[k]; t.Static {
			restartRequired = true
		}
	}

	return restartRequired, changed, removed
}

func (p *Process) applyTunables(config *state.Config) error {
	logger := p.Logger.New("fn", "applyTunables")
	p.writeConfig(configData{
		ReadOnly: config.Role != state.RolePrimary,
		Tunables: config.Tunables.Data,
	})

	// If postgres is running and the tunables require a restart then
	// restart the postgres process. If not running we do nothing.
	restartRequired, changed, removed := p.tunablesRequireRestart(config)
	if restartRequired {
		logger.Info("restarting database to apply tunables")
		if err := p.stop(); err != nil {
			return err
		}
		if err := p.start(); err != nil {
			return err
		}
	} else {
		logger.Info("applying tunables online")
		// Connect to dynamically reconfigure variables
		db, err := p.connectLocal()
		if err != nil {
			logger.Error("error acquiring connection", "err", err)
			return err
		}
		defer db.Close()
		// Update changed variables
		for k := range changed {
			v := config.Tunables.Data[k]
			if _, err := db.Exec(fmt.Sprintf(`SET GLOBAL %s = %s`, k, v)); err != nil {
				logger.Error("error setting system variable", "var", k, "val", v, "err", err)
				return err
			}
		}
		// Set removed variables back to defaults
		for k := range removed {
			v := allowedTunables[k].Default
			if _, err := db.Exec(fmt.Sprintf(`SET GLOBAL %s = %s`, k, v)); err != nil {
				logger.Error("error resetting system variable", "var", k, "default", v, "err", err)
				return err
			}
		}
	}
	return nil
}

func (p *Process) XLog() xlog.XLog {
	return mdbxlog.MDBXLog{}
}

func (p *Process) reconfigure(config *state.Config) error {
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

		// If only tunables have been updated apply them and return.
		if p.running() && p.config().IsTunablesUpdate(config) {
			logger.Info("tunables only update")
			return p.applyTunables(config)
		}

		// If we're already running and it's just a change from async to sync with the same node, we don't need to restart
		if p.configApplied && p.running() && p.config() != nil && config != nil &&
			p.config().Role == state.RoleAsync && config.Role == state.RoleSync && config.Upstream.Meta["MYSQL_ID"] == p.config().Upstream.Meta["MYSQL_ID"] {
			// If the tunables haven't been modified there is nothing to do here
			if config.Tunables.Version > p.config().Tunables.Version {
				logger.Info("becoming sync with same upstream and updating tunables")
				return p.applyTunables(config)
			}
			logger.Info("nothing to do", "reason", "becoming sync with same upstream")
			return nil
		}

		// Make sure that we don't keep waiting for replication sync while reconfiguring
		p.cancelSyncWait()
		p.syncedDownstreamValue.Store((*discoverd.Instance)(nil))

		// If we're already running and this is only a downstream change, just wait for the new downstream to catch up
		if p.running() && p.config().IsNewDownstream(config) {
			logger.Info("downstream changed", "to", config.Downstream.Addr)
			var err error
			if config.Tunables.Version > p.config().Tunables.Version {
				logger.Info("updating tunables")
				err = p.applyTunables(config)
			}
			p.waitForSync(config.Downstream, config.Role == state.RolePrimary)
			return err
		}

		if config == nil {
			config = p.config()
		}

		if config.Role == state.RolePrimary {
			return p.assumePrimary(config)
		}

		return p.assumeStandby(config)
	}(); err != nil {
		return err
	}

	// Apply configuration.
	p.configValue.Store(config)
	p.configApplied = true

	return nil
}

func (p *Process) assumePrimary(config *state.Config) (err error) {
	logger := p.Logger.New("fn", "assumePrimary")
	downstream := config.Downstream
	if downstream != nil {
		logger = logger.New("downstream", downstream.Addr)
	}

	if p.running() && p.config().Role == state.RoleSync {
		logger.Info("promoting to primary")
		p.waitForSync(downstream, true)
		return nil
	}

	logger.Info("starting as primary")

	// Assert that the process is not running. This should not occur.
	if p.running() {
		panic(fmt.Sprintf("unexpected state running role=%s", p.config().Role))
	}

	if err := p.writeConfig(configData{ReadOnly: downstream != nil, Tunables: config.Tunables.Data}); err != nil {
		logger.Error("error writing config", "path", p.ConfigPath(), "err", err)
		return err
	}

	if err := p.installDB(); err != nil {
		return err
	}

	if err := p.start(); err != nil {
		return err
	}

	if err := p.initPrimaryDB(); err != nil {
		if e := p.stop(); err != nil {
			logger.Debug("ignoring error stopping process", "err", e)
		}
		return err
	}

	if downstream != nil {
		p.waitForSync(downstream, true)
	}

	return nil
}

// Backup returns a reader for streaming a backup in xbstream format.
func (p *Process) Backup() (io.ReadCloser, error) {
	r := &backupReadCloser{}

	cmd := exec.Command(
		filepath.Join(p.BinDir, "innobackupex"),
		"--defaults-file="+p.ConfigPath(),
		"--host=127.0.0.1",
		"--port="+p.Port,
		"--user=flynn",
		"--password="+p.Password,
		"--socket=",
		"--stream=xbstream",
		".",
	)
	cmd.Dir = p.DataDir
	cmd.Stderr = &r.stderr

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		stdout.Close()
		return nil, err
	}

	// Attach to reader wrapper.
	r.cmd = cmd
	r.stdout = stdout

	return r, nil
}

type BackupInfo struct {
	LogFile string
	LogPos  string
	GTID    string
}

func (p *Process) extractBackupInfo() (*BackupInfo, error) {
	buf, err := ioutil.ReadFile(filepath.Join(p.DataDir, "xtrabackup_binlog_info"))
	if err != nil {
		return nil, err
	}
	fields := strings.Fields(string(buf))
	if len(fields) < 3 {
		return nil, fmt.Errorf("malformed xtrabackup_binlog_info, len %d", len(fields))
	}
	return &BackupInfo{LogFile: fields[0], LogPos: fields[1], GTID: fields[2]}, nil
}

// Restore restores the database from an xbstream backup.
func (p *Process) Restore(r io.Reader) (*BackupInfo, error) {
	if err := p.writeConfig(configData{}); err != nil {
		return nil, err
	}
	if err := p.unpackXbstream(r); err != nil {
		return nil, err
	}
	backupInfo, err := p.extractBackupInfo()
	if err != nil {
		return nil, err
	}
	if err := p.restoreApplyLog(); err != nil {
		return nil, err
	}
	return backupInfo, nil
}

func (p *Process) unpackXbstream(r io.Reader) error {
	cmd := exec.Command(filepath.Join(p.BinDir, "xbstream"), "-x", "--directory="+p.DataDir)
	cmd.Stdin = ioutil.NopCloser(r)

	if buf, err := cmd.CombinedOutput(); err != nil {
		p.Logger.Error("xbstream failed", "err", err, "output", string(buf))
		return err
	}

	return nil
}

func (p *Process) restoreApplyLog() error {
	cmd := exec.Command(
		filepath.Join(p.BinDir, "innobackupex"),
		"--defaults-file="+p.ConfigPath(),
		"--apply-log",
		p.DataDir,
	)
	if buf, err := cmd.CombinedOutput(); err != nil {
		p.Logger.Error("innobackupex apply-log failed", "err", err, "output", string(buf))
		return err
	}
	return nil
}

func (p *Process) assumeStandby(config *state.Config) error {
	upstream := config.Upstream
	downstream := config.Downstream
	logger := p.Logger.New("fn", "assumeStandby", "upstream", upstream.Addr)
	logger.Info("starting up as standby")

	if err := p.writeConfig(configData{ReadOnly: true, Tunables: config.Tunables.Data}); err != nil {
		logger.Error("error writing config", "path", p.ConfigPath(), "err", err)
		return err
	}

	var backupInfo *BackupInfo
	if p.running() {
		if err := p.stop(); err != nil {
			return err
		}
	} else {
		if err := p.waitForUpstream(upstream); err != nil {
			return err
		}

		if err := func() error {
			logger.Info("retrieving backup")
			resp, err := http.Get(fmt.Sprintf("http://%s/backup", httpAddr(upstream.Addr)))
			if err != nil {
				logger.Error("error connecting to upstream for backup", "err", err)
				return err
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				logger.Error("error code returned from backup", "status_code", resp.StatusCode)
				return err
			}

			hash := sha512.New()

			logger.Info("restoring backup")
			backupInfo, err = p.Restore(io.TeeReader(resp.Body, hash))
			if err != nil {
				logger.Error("error restoring backup", "err", err)
				return err
			}

			// Close response and confirm backup from trailer.
			if err := resp.Body.Close(); err != nil {
				logger.Error("error closing backup body", "err", err)
				return err
			}

			chk := hex.EncodeToString(hash.Sum(nil))
			logger.Error("verifying backup checksum", "actual", chk)
			if hdr := resp.Trailer.Get(backupChecksumTrailer); hdr != chk {
				logger.Error("invalid backup checksum", "actual", chk, "desired", hdr)
				return errors.New("invalid backup")
			}

			return nil
		}(); err != nil {
			if files, err := ioutil.ReadDir("/data"); err == nil {
				for _, file := range files {
					os.RemoveAll(filepath.Join("/data", file.Name()))
				}
			}
			return err
		}
	}

	if err := p.start(); err != nil {
		return err
	}

	if err := func() error {
		// Connect to local server and set up slave replication.
		db, err := p.connectLocal()
		if err != nil {
			logger.Error("error acquiring connection", "err", err)
			return err
		}
		defer db.Close()

		// Stop the slave first before changing GTID & MASTER settings.
		if _, err := db.Exec(`STOP SLAVE`); err != nil {
			return err
		}

		// Install semi-sync slave plugin. Ignore error if already installed.
		if _, err := db.Exec(`INSTALL PLUGIN rpl_semi_sync_slave SONAME 'semisync_slave.so'`); err != nil && MySQLErrorNumber(err) != 1968 {
			logger.Error("error installing rpl_semi_sync_slave", "err", err)
			return err
		}

		// Enable semi-synchronous on slave.
		if _, err := db.Exec(`SET GLOBAL rpl_semi_sync_slave_enabled = 1`); err != nil {
			return err
		}

		// Only update the GTID if we read from a backup.
		if backupInfo != nil {
			logger.Info("updating gtid_slave_pos", "gtid", backupInfo.GTID)
			if _, err := db.Exec(fmt.Sprintf(`SET GLOBAL gtid_slave_pos = "%s";`, backupInfo.GTID)); err != nil {
				logger.Error("error updating slave gtid")
				return err
			}
		}

		host, port, _ := net.SplitHostPort(upstream.Addr)
		logger.Info("changing master", "host", host, "port", port)
		if _, err := db.Exec(fmt.Sprintf("CHANGE MASTER TO MASTER_HOST='%s', MASTER_PORT=%s, MASTER_USER='flynn', MASTER_PASSWORD='%s', MASTER_CONNECT_RETRY=10, MASTER_USE_GTID=current_pos;", host, port, p.Password)); err != nil {
			logger.Error("error changing master", "host", host, "port", port, "err", err)
			return err
		}
		if _, err := db.Exec(`STOP SLAVE IO_THREAD`); err != nil {
			logger.Error("error stopping slave io thread", "err", err)
			return err
		}
		if _, err := db.Exec(`START SLAVE IO_THREAD`); err != nil {
			logger.Error("error starting slave io thread", "err", err)
			return err
		}

		// Start slave.
		logger.Info("starting slave")
		if _, err := db.Exec(`START SLAVE`); err != nil {
			return err
		}

		return nil
	}(); err != nil {
		return err
	}

	if downstream != nil {
		p.waitForSync(downstream, false)
	}

	return nil
}

// initPrimaryDB initializes the local database with the correct users and plugins.
func (p *Process) initPrimaryDB() error {
	logger := p.Logger.New("fn", "initPrimaryDB")
	logger.Info("initializing primary database")

	dsn := &DSN{
		Host:    "127.0.0.1:" + p.Port,
		User:    "root",
		Timeout: p.OpTimeout,
	}

	db, err := sql.Open("mysql", dsn.String())
	if err != nil {
		logger.Error("error acquiring connection", "err", err)
		return err
	}
	defer db.Close()

	if _, err := db.Exec(fmt.Sprintf(`CREATE USER 'flynn'@'%%' IDENTIFIED BY '%s'`, p.Password)); err != nil && MySQLErrorNumber(err) != 1396 {
		logger.Error("error creating database user", "err", err)
		return err
	}
	if _, err := db.Exec(`GRANT ALL ON *.* TO 'flynn'@'%' WITH GRANT OPTION`); err != nil {
		logger.Error("error granting privileges", "err", err)
		return err
	}
	if _, err := db.Exec(`INSTALL PLUGIN rpl_semi_sync_master SONAME 'semisync_master.so'`); err != nil && MySQLErrorNumber(err) != 1968 {
		logger.Error("error installing rpl_semi_sync_master", "err", err)
		return err
	}
	if _, err := db.Exec(`FLUSH PRIVILEGES`); err != nil {
		logger.Error("error flushing privileges", "err", err)
		return err
	}

	// If we are running in Singleton mode we don't need to setup replication
	if p.Singleton {
		return nil
	}
	// Enable semi-sync replication on the master.
	master_variables := map[string]string{
		"rpl_semi_sync_master_wait_point":    "AFTER_SYNC",
		"rpl_semi_sync_master_timeout":       "18446744073709551615",
		"rpl_semi_sync_master_enabled":       "1",
		"rpl_semi_sync_master_wait_no_slave": "1",
	}

	for v, val := range master_variables {
		if _, err := db.Exec(fmt.Sprintf(`SET GLOBAL %s = %s`, v, val)); err != nil {
			logger.Error("error setting system variable", "var", v, "val", val, "err", err)
			return err
		}
	}

	return nil
}

// upstreamTimeout is of the order of the discoverd heartbeat to prevent
// waiting for an upstream which has gone down.
var upstreamTimeout = 10 * time.Second

func httpAddr(addr string) string {
	host, p, _ := net.SplitHostPort(addr)
	port, _ := strconv.Atoi(p)
	return fmt.Sprintf("%s:%d", host, port+1)
}

func (p *Process) waitForUpstream(upstream *discoverd.Instance) error {
	logger := p.Logger.New("fn", "waitForUpstream", "upstream", upstream.Addr, "upstream_http_addr", httpAddr(upstream.Addr))
	logger.Info("waiting for upstream to come online")
	upstreamClient := client.NewClient(upstream.Addr)

	timer := time.NewTimer(upstreamTimeout)
	defer timer.Stop()

	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()

	for {
		status, err := upstreamClient.Status()
		if err != nil {
			logger.Error("error getting upstream status", "err", err)
		} else if status.Database != nil {
			logger.Info("status", "running", status.Database.Running, "xlog", status.Database.XLog, "user_exists", status.Database.UserExists)
			if status.Database.Running && status.Database.XLog != "" && status.Database.UserExists {
				logger.Info("upstream is online")
				return nil
			}
		} else {
			logger.Info("status", "running", "false")
		}

		select {
		case <-timer.C:
			logger.Error("upstream did not come online in time")
			return errors.New("upstream is offline")
		case <-ticker.C:
		}
	}
}

func (p *Process) connectLocal() (*sql.DB, error) {
	dsn := p.DSN()
	dsn.User = "root"
	dsn.Password = ""

	db, err := sql.Open("mysql", dsn.String())
	if err != nil {
		return nil, err
	}
	return db, nil
}

func (p *Process) start() error {
	logger := p.Logger.New("fn", "start", "id", p.ID, "port", p.Port)
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
			db, err := p.connectLocal()
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

func (p *Process) Info() (*client.DatabaseInfo, error) {
	info := &client.DatabaseInfo{
		Config:           p.config(),
		Running:          p.running(),
		SyncedDownstream: p.syncedDownstream(),
	}

	xlog, err := p.XLogPosition()
	info.XLog = string(xlog)
	if err != nil {
		return info, err
	}

	info.UserExists, err = p.userExists()
	if err != nil {
		return info, err
	}
	info.ReadWrite, err = p.isReadWrite()
	if err != nil {
		return info, err
	}
	return info, err
}

func (p *Process) isReadWrite() (bool, error) {
	if !p.running() {
		return false, nil
	}
	db, err := p.connectLocal()
	if err != nil {
		return false, err
	}
	defer db.Close()
	var readOnly string
	if err := db.QueryRow("SELECT @@read_only").Scan(&readOnly); err != nil {
		return false, err
	}
	return readOnly == "0", nil
}

func (p *Process) userExists() (bool, error) {
	if !p.running() {
		return false, errors.New("mariadb is not running")
	}

	db, err := p.connectLocal()
	if err != nil {
		return false, err
	}
	defer db.Close()

	var res sql.NullInt64
	if err := db.QueryRow("SELECT 1 FROM mysql.user WHERE User='flynn'").Scan(&res); err != nil {
		return false, err
	}
	return res.Valid, nil
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

		startTime := time.Now().UTC()
		logger := p.Logger.New(
			"fn", "waitForSync",
			"sync_name", downstream.Meta["MYSQL_ID"],
			"start_time", log15.Lazy{func() time.Time { return startTime }},
		)

		logger.Info("waiting for downstream replication to catch up")
		defer logger.Info("finished waiting for downstream replication")

		prevSlaveXLog := p.XLog().Zero()
		for {
			// Check if "wait sync" has been canceled.
			select {
			case <-stopCh:
				logger.Debug("canceled, stopping")
				return
			default:
			}

			// Read local master status.
			masterXLog, err := p.XLogPosition()
			if err != nil {
				logger.Error("error reading master xlog", "err", err)
				startTime = time.Now().UTC()
				select {
				case <-stopCh:
					logger.Debug("canceled, stopping")
					return
				case <-time.After(checkInterval):
				}
				continue
			}
			logger.Info("master xlog", "gtid", masterXLog)

			// Read downstream slave status.
			slaveXLog, err := p.nodeXLogPosition(&DSN{
				Host:     downstream.Addr,
				User:     "flynn",
				Password: p.Password,
				Timeout:  p.OpTimeout,
			})
			if err != nil {
				logger.Error("error reading slave xlog", "err", err)
				startTime = time.Now().UTC()
				select {
				case <-stopCh:
					logger.Debug("canceled, stopping")
					return
				case <-time.After(checkInterval):
				}
				continue
			}

			logger.Info("mysql slave xlog", "gtid", slaveXLog)

			elapsedTime := time.Since(startTime)
			logger := logger.New(
				"master_log_pos", masterXLog,
				"slave_log_pos", slaveXLog,
				"elapsed", elapsedTime,
			)

			// Mark downstream server as synced if the xlog matches the master.
			if cmp, err := p.XLog().Compare(masterXLog, slaveXLog); err == nil && cmp == 0 {
				logger.Info("downstream caught up")
				p.syncedDownstreamValue.Store(downstream)
				break
			}

			// If the slave's xlog is making progress then reset the start time.
			if cmp, err := p.XLog().Compare(prevSlaveXLog, slaveXLog); err == nil && cmp == -1 {
				logger.Debug("slave status progressing, resetting start time")
				startTime = time.Now().UTC()
			}
			prevSlaveXLog = slaveXLog

			if elapsedTime > p.ReplTimeout {
				logger.Error("error checking replication status", "err", "downstream unable to make forward progress")
				return
			}

			logger.Debug("continuing replication check")
			select {
			case <-stopCh:
				logger.Debug("canceled, stopping")
				return
			case <-time.After(checkInterval):
			}
		}

		if enableWrites {
			db, err := p.connectLocal()
			if err != nil {
				logger.Error("error acquiring connection", "err", err)
				return
			}
			defer db.Close()
			if _, err := db.Exec(`SET GLOBAL read_only = 0`); err != nil {
				logger.Error("error setting database read/write", "err", err)
				return
			}
		}
	}()
}

// DSN returns the data source name for connecting to the local process as the "flynn" user.
func (p *Process) DSN() *DSN {
	return &DSN{
		Host:     "127.0.0.1:" + p.Port,
		User:     "flynn",
		Password: p.Password,
		Timeout:  p.OpTimeout,
	}
}

func (p *Process) XLogPosition() (xlog.Position, error) {
	return p.nodeXLogPosition(p.DSN())
}

// XLogPosition returns the current XLogPosition of node specified by DSN.
func (p *Process) nodeXLogPosition(dsn *DSN) (xlog.Position, error) {
	db, err := sql.Open("mysql", dsn.String())
	if err != nil {
		return p.XLog().Zero(), err
	}
	defer db.Close()

	var gtid string
	if err := db.QueryRow(`SELECT @@gtid_current_pos`).Scan(&gtid); err != nil {
		return p.XLog().Zero(), err
	}
	return xlog.Position(gtid), nil

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
	d.ServerID = p.ServerID

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
	DataDir  string
	ServerID uint32
	ReadOnly bool
	Tunables map[string]string
}

var configTemplate = template.Must(template.New("my.cnf").Parse(`
[client]
port = {{.Port}}

[mysqld]
user         = ""
port         = {{.Port}}
bind_address = 0.0.0.0
server_id    = {{.ServerID}}
socket       = ""
pid_file     = {{.DataDir}}/mysql.pid
report_host  = {{.ID}}

datadir             = {{.DataDir}}
log_bin             = {{.DataDir}}/mariadb-bin
log_bin_index       = {{.DataDir}}/mariadb-bin.index
log_slave_updates   = 1

{{if .ReadOnly}}
read_only = 1
{{end}}

{{ range $key, $value := .Tunables }}
{{ $key }} = {{ $value }}
{{ end }}
`[1:]))

// MySQLErrorNumber returns the Number field from err if it is a *mysql.Error.
// Returns 0 for non-mysql error types.
func MySQLErrorNumber(err error) uint16 {
	if err, ok := err.(*mysql.MySQLError); ok {
		return err.Number
	}
	return 0
}

// backupReadCloser wraps the Cmd of the innobackupex to perform error handling.
type backupReadCloser struct {
	cmd    *exec.Cmd
	stdout io.ReadCloser
	stderr bytes.Buffer
}

// Close waits for the backup command to finish and verifies that the backup completed successfully.
func (r *backupReadCloser) Close() error {
	defer r.stdout.Close()

	if err := r.cmd.Wait(); err != nil {
		return err
	}

	// Verify that innobackupex prints "completed OK!" at the end of STDERR.
	if !strings.HasSuffix(strings.TrimSpace(r.stderr.String()), "completed OK!") {
		r.stderr.WriteTo(os.Stderr)
		return errors.New("innobackupex did not complete ok")
	}

	return nil
}

// Read reads n bytes of backup data into p.
func (r *backupReadCloser) Read(p []byte) (n int, err error) {
	return r.stdout.Read(p)
}
