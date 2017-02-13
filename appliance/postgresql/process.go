package postgresql

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"

	"text/template"
	"time"

	"github.com/flynn/flynn/appliance/postgresql/pgxlog"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/shutdown"
	"github.com/flynn/flynn/pkg/sirenia/client"
	"github.com/flynn/flynn/pkg/sirenia/state"
	"github.com/flynn/flynn/pkg/sirenia/xlog"
	"github.com/jackc/pgx"
	"gopkg.in/inconshreveable/log15.v2"
)

const (
	IDKey = "POSTGRES_ID"
)

type Config struct {
	ID           string
	Singleton    bool
	Port         string
	BinDir       string
	DataDir      string
	Password     string
	OpTimeout    time.Duration
	ReplTimeout  time.Duration
	Logger       log15.Logger
	ExtWhitelist bool
	WaitUpstream bool
}

type Process struct {
	// dbMtx is locked while db is being changed
	dbMtx sync.RWMutex
	db    *pgx.ConnPool

	events chan state.DatabaseEvent

	configVal           atomic.Value // *state.Config
	runningVal          atomic.Value // bool
	syncedDownstreamVal atomic.Value // *discoverd.Instance
	configApplied       bool

	// config options
	id           string
	log          log15.Logger
	singleton    bool
	port         string
	binDir       string
	dataDir      string
	password     string
	opTimeout    time.Duration
	replTimeout  time.Duration
	extWhitelist bool
	waitUpstream bool

	// daemon is the postgres daemon command when running
	daemon *exec.Cmd
	// expectExit is bool, true if the daemon is supposed to exit
	expectExit atomic.Value
	// daemonExit is closed when the daemon exits
	daemonExit chan struct{}

	// cancelSyncWait cancels the goroutine that is waiting for the downstream to
	// catch up, if running
	cancelSyncWait func()

	// mtx ensures that only one operation happens at a time
	mtx sync.Mutex
}

const checkInterval = 100 * time.Millisecond

func NewProcess(c Config) *Process {
	p := &Process{
		id:             c.ID,
		log:            c.Logger,
		singleton:      c.Singleton,
		port:           c.Port,
		binDir:         c.BinDir,
		dataDir:        c.DataDir,
		password:       c.Password,
		opTimeout:      c.OpTimeout,
		replTimeout:    c.ReplTimeout,
		extWhitelist:   c.ExtWhitelist,
		waitUpstream:   c.WaitUpstream,
		events:         make(chan state.DatabaseEvent, 1),
		cancelSyncWait: func() {},
	}
	p.setRunning(false)
	p.setConfig(nil)
	p.setSyncedDownstream(nil)
	if p.log == nil {
		p.log = log15.New("app", "postgres", "id", p.id)
	}
	if p.port == "" {
		p.port = "5432"
	}
	if p.binDir == "" {
		p.binDir = "/usr/lib/postgresql/9.5/bin/"
	}
	if p.dataDir == "" {
		p.dataDir = "/data"
	}
	if p.password == "" {
		p.password = "password"
	}
	if p.opTimeout == 0 {
		p.opTimeout = 5 * time.Minute
	}
	if p.replTimeout == 0 {
		p.replTimeout = 1 * time.Minute
	}
	p.events <- state.DatabaseEvent{}
	return p
}

func (p *Process) XLog() xlog.XLog {
	return pgxlog.PgXLog{}
}

func (p *Process) running() bool {
	return p.runningVal.Load().(bool)
}

func (p *Process) setRunning(running bool) {
	p.runningVal.Store(running)
}

func (p *Process) config() *state.Config {
	return p.configVal.Load().(*state.Config)
}

func (p *Process) setConfig(config *state.Config) {
	p.configVal.Store(config)
}

func (p *Process) syncedDownstream() *discoverd.Instance {
	return p.syncedDownstreamVal.Load().(*discoverd.Instance)
}

func (p *Process) setSyncedDownstream(inst *discoverd.Instance) {
	p.syncedDownstreamVal.Store(inst)
}

func (p *Process) Info() (*client.DatabaseInfo, error) {
	res := &client.DatabaseInfo{
		Config:           p.config(),
		Running:          p.running(),
		SyncedDownstream: p.syncedDownstream(),
	}
	xlog, err := p.XLogPosition()
	res.XLog = string(xlog)
	if err != nil {
		return res, err
	}
	res.UserExists, err = p.userExists()
	if err != nil {
		return res, err
	}
	res.ReadWrite, err = p.isReadWrite()
	if err != nil {
		return res, err
	}
	return res, err
}

func (p *Process) Reconfigure(config *state.Config) error {
	p.mtx.Lock()
	defer p.mtx.Unlock()

	switch config.Role {
	case state.RolePrimary:
		if !p.singleton && config.Downstream == nil {
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
		p.setConfig(config)
		p.configApplied = false
		return nil
	}

	return p.reconfigure(config)
}

func (p *Process) Start() error {
	p.mtx.Lock()
	defer p.mtx.Unlock()

	if p.running() {
		return errors.New("postgres is already running")
	}
	if p.config() == nil {
		return errors.New("postgres is unconfigured")
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
		return errors.New("postgres is already stopped")
	}
	return p.stop()
}

func (p *Process) XLogPosition() (xlog.Position, error) {
	p.dbMtx.RLock()
	defer p.dbMtx.RUnlock()

	if !p.running() || p.db == nil {
		return "", errors.New("postgres is not running")
	}

	fn := "pg_last_xlog_replay_location()"
	if p.config().Role == state.RolePrimary {
		fn = "pg_current_xlog_location()"
	}
	var res string
	err := p.db.QueryRow("SELECT " + fn).Scan(&res)
	return xlog.Position(res), err
}

func (p *Process) userExists() (bool, error) {
	p.dbMtx.RLock()
	defer p.dbMtx.RUnlock()

	if !p.running() || p.db == nil {
		return false, errors.New("postgres is not running")
	}
	var res pgx.NullInt32
	err := p.db.QueryRow("SELECT 1 FROM pg_roles WHERE rolname='flynn'").Scan(&res)
	return res.Valid, err
}

func (p *Process) isReadWrite() (bool, error) {
	p.dbMtx.RLock()
	defer p.dbMtx.RUnlock()

	if !p.running() || p.db == nil {
		return false, errors.New("postgres is not running")
	}
	var res string
	err := p.db.QueryRow("SHOW default_transaction_read_only").Scan(&res)
	return res == "off", err
}

func (p *Process) Ready() <-chan state.DatabaseEvent {
	return p.events
}

func (p *Process) DefaultTunables() state.Tunables {
	return state.Tunables{
		Data: map[string]string{
			"dynamic_shared_memory_type":   "posix",
			"shared_buffers":               "32MB",
			"max_wal_senders":              "15",
			"wal_keep_segments":            "128",
			"max_standby_archive_delay":    "30s",
			"max_standby_streaming_delay":  "30s",
			"wal_receiver_status_interval": "10s",
			"datestyle":                    "'iso, mdy'",
			"timezone":                     "'UTC'",
			"client_encoding":              "'UTF8'",
			"log_timezone":                 "'UTC'",
			"log_min_messages":             "'LOG'",
			"log_connections":              "on",
			"log_disconnections":           "on",
			"default_text_search_config":   "'pg_catalog.english'",
		},
		Version: 1,
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

func (p *Process) tunablesRequireRestart(config *state.Config) bool {
	curTunables := p.config().Tunables.Data
	newTunables := config.Tunables.Data
	changed := make(map[string]struct{})

	// iterate over current to find any changed or removed variables
	for k, v := range curTunables {
		if newTunables[k] != v {
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
		if allowedTunables[k] {
			restartRequired = true
		}
	}

	return restartRequired
}

func (p *Process) applyTunables(config *state.Config) error {
	log := p.log.New("fn", "applyTunables")
	var readOnly bool
	var sync string
	if config.Role == state.RolePrimary {
		if config.Downstream != nil {
			sync = config.Downstream.Meta[IDKey]
		}
	} else {
		readOnly = true
	}
	p.writeConfig(configData{
		ReadOnly: readOnly,
		Sync:     sync,
		Tunables: config.Tunables.Data,
	})

	// If postgres is running and the tunables require a restart then
	// restart the postgres process. If not running we do nothing.
	if p.tunablesRequireRestart(config) {
		log.Info("restarting database to apply tunables")
		if err := p.stop(); err != nil {
			return err
		}
		if err := p.start(); err != nil {
			return err
		}
	} else {
		log.Info("applying tunables online")
		p.sighup()
	}
	return nil
}

func (p *Process) reconfigure(config *state.Config) (err error) {
	log := p.log.New("fn", "reconfigure")

	defer func() {
		if err == nil {
			p.setConfig(config)
			p.configApplied = true
		}
	}()

	if config != nil && config.Role == state.RoleNone {
		log.Info("nothing to do", "reason", "null role")
		return nil
	}

	// If we've already applied the same postgres config, we don't need to do anything
	if p.configApplied && config != nil && p.config() != nil && config.Equal(p.config()) {
		log.Info("nothing to do", "reason", "config already applied")
		return nil
	}

	// If only tunables have been updated then apply them and return.
	if p.running() && p.config().IsTunablesUpdate(config) {
		log.Info("tunables only update")
		return p.applyTunables(config)
	}

	// If we're already running and it's just a change from async to sync with the same node, we don't need to restart
	if p.configApplied && p.running() && p.config() != nil && config != nil &&
		p.config().Role == state.RoleAsync && config.Role == state.RoleSync && config.Upstream.Meta[IDKey] == p.config().Upstream.Meta[IDKey] {
		// Check to see if we should update tunables
		if config.Tunables.Version > p.config().Tunables.Version {
			log.Info("becoming sync with same upstream and updating tunables")
			return p.applyTunables(config)
		}
		// If the tunables haven't been modified there is nothing to do here
		log.Info("nothing to do", "reason", "becoming sync with same upstream")
		return nil
	}

	// Make sure that we don't keep waiting for replication sync while reconfiguring
	p.cancelSyncWait()
	p.setSyncedDownstream(nil)

	// If we're already running and this is only a sync change, we just need to update the config.
	if p.running() && p.config() != nil && config != nil && p.config().Role == state.RolePrimary && config.Role == state.RolePrimary {
		return p.updateSync(config)
	}

	// If we're already running and this is only a downstream change, just wait for the new downstream to catch up
	if p.running() && p.config().IsNewDownstream(config) {
		log.Info("downstream changed", "to", config.Downstream.Addr)
		// Check to see if we should update tunables before we wait on the new downstream
		if config.Tunables.Version > p.config().Tunables.Version {
			log.Info("updating tunables")
			err = p.applyTunables(config)
		}
		p.waitForSync(config.Downstream, config, false)
		return err
	}

	if config == nil {
		config = p.config()
	}

	if config.Role == state.RolePrimary {
		return p.assumePrimary(config)
	}

	return p.assumeStandby(config)
}

func (p *Process) assumePrimary(config *state.Config) (err error) {
	log := p.log.New("fn", "assumePrimary")
	downstream := config.Downstream
	if downstream != nil {
		log = log.New("downstream", downstream.Addr)
	}

	if p.running() && p.config().Role == state.RoleSync {
		log.Info("promoting to primary")

		if err := ioutil.WriteFile(p.triggerPath(), nil, 0655); err != nil {
			log.Error("error creating trigger file", "path", p.triggerPath(), "err", err)
			return err
		}

		p.waitForSync(downstream, config, true)

		return nil
	}

	log.Info("starting as primary")

	if p.running() {
		panic(fmt.Sprintf("unexpected state running role=%s", p.config().Role))
	}

	if err := p.initDB(); err != nil {
		return err
	}

	if err := os.Remove(p.recoveryConfPath()); err != nil && !os.IsNotExist(err) {
		log.Error("error removing recovery.conf", "path", p.recoveryConfPath(), "err", err)
		return err
	}

	if err := p.writeConfig(configData{ReadOnly: downstream != nil, Tunables: config.Tunables.Data}); err != nil {
		log.Error("error writing postgres.conf", "path", p.configPath(), "err", err)
		return err
	}

	if err := p.start(); err != nil {
		return err
	}

	var tx *pgx.Tx
	defer func() {
		if err != nil {
			if tx != nil {
				tx.Rollback()
			}
			p.db.Close()
			if err := p.stop(); err != nil {
				log.Debug("ignoring error stopping postgres", "err", err)
			}
		}
	}()

	tx, err = p.db.Begin()
	if err != nil {
		log.Error("error acquiring connection", "err", err)
		return err
	}
	if _, err := tx.Exec("SET TRANSACTION READ WRITE"); err != nil {
		log.Error("error setting transaction read-write", "err", err)
		return err
	}
	if _, err := tx.Exec(fmt.Sprintf(`
		DO
		$body$
		BEGIN
		   IF NOT EXISTS (
			  SELECT * FROM pg_catalog.pg_user
			  WHERE	usename = 'flynn')
		   THEN
			  CREATE USER flynn WITH SUPERUSER CREATEDB CREATEROLE REPLICATION PASSWORD '%s';
		   END IF;
		END
		$body$;
	`, p.password)); err != nil {
		log.Error("error creating superuser", "err", err)
		return err
	}
	if err := tx.Commit(); err != nil {
		log.Error("error committing transaction", "err", err)
		return err
	}

	if downstream != nil {
		p.waitForSync(downstream, config, true)
	}

	return nil
}

func (p *Process) assumeStandby(config *state.Config) error {
	upstream := config.Upstream
	downstream := config.Downstream
	log := p.log.New("fn", "assumeStandby", "upstream", upstream.Addr)
	log.Info("starting up as standby")

	// TODO(titanous): investigate using a DNS name, proxy, or iptables rule for
	// the upstream. (perhaps DNS plus some postgres magic like terminating
	// a backend would work). Postgres appears to support remastering without
	// restarting:
	// http://www.databasesoup.com/2014/05/remastering-without-restarting.html

	if p.running() {
		// if we are running, we can just restart with a new recovery.conf, postgres
		// supports streaming remastering.
		if err := p.stop(); err != nil {
			return err
		}
	} else {
		if p.waitUpstream {
			if err := p.waitForUpstream(upstream); err != nil {
				return err
			}
		}
		log.Info("pulling basebackup")
		// TODO(titanous): make this pluggable
		err := p.runCmd(exec.Command(
			p.binPath("pg_basebackup"),
			"--pgdata", p.dataDir,
			"--dbname", fmt.Sprintf(
				"host=%s port=%s user=flynn password=%s application_name=%s",
				upstream.Host(), upstream.Port(), p.password, upstream.Meta[IDKey],
			),
			"--xlog-method=stream",
			"--progress",
			"--verbose",
		))
		if err != nil {
			log.Error("error pulling basebackup", "err", err)
			if files, err := ioutil.ReadDir("/data"); err == nil {
				for _, file := range files {
					os.RemoveAll(filepath.Join("/data", file.Name()))
				}
			}
			return err
		}
		// the upstream could be performing a takeover, so we need to
		// remove the trigger file if we have synced it across so we
		// don't also start a takeover.
		os.Remove(p.triggerPath())
	}

	if err := p.writeConfig(configData{ReadOnly: true, Tunables: config.Tunables.Data}); err != nil {
		log.Error("error writing postgres.conf", "path", p.configPath(), "err", err)
		return err
	}
	if err := p.writeRecoveryConf(upstream); err != nil {
		log.Error("error writing recovery.conf", "path", p.recoveryConfPath(), "err", err)
		return err
	}

	if err := p.start(); err != nil {
		return err
	}

	if downstream != nil {
		p.waitForSync(downstream, config, false)
	}

	return nil
}

// upstreamTimeout is of the order of the discoverd heartbeat to prevent
// waiting for an upstream which has gone down.
var upstreamTimeout = 10 * time.Second

func (p *Process) waitForUpstream(upstream *discoverd.Instance) error {
	log := p.log.New("fn", "waitForUpstream", "upstream", upstream.Addr)
	log.Info("waiting for upstream to come online")
	client := client.NewClient(upstream.Addr)

	timeout := time.After(upstreamTimeout)
	for {
		status, err := client.Status()
		if err != nil {
			log.Error("error getting upstream status", "err", err)
		} else if status.Database != nil && status.Database.Running && status.Database.XLog != "" && status.Database.UserExists {
			log.Info("upstream is online")
			return nil
		}
		select {
		case <-timeout:
			log.Error("upstream did not come online in time")
			return errors.New("upstream is offline")
		case <-time.After(checkInterval):
		}
	}
}

func (p *Process) updateSync(config *state.Config) error {
	downstream := config.Downstream
	log := p.log.New("fn", "updateSync", "downstream", downstream.Addr)
	log.Info("changing sync")

	if err := p.writeConfig(configData{ReadOnly: true, Sync: downstream.Meta[IDKey], Tunables: config.Tunables.Data}); err != nil {
		log.Error("error writing postgres.conf", "path", p.configPath(), "err", err)
		return err
	}

	if err := p.sighup(); err != nil {
		p.log.Error("error reloading daemon configuration", "err", err)
		return err
	}

	p.waitForSync(downstream, config, true)

	return nil
}

func (p *Process) start() error {
	log := p.log.New("fn", "start", "data_dir", p.dataDir, "bin_dir", p.binDir)
	log.Info("starting postgres")

	// clear stale pid if it exists
	os.Remove(filepath.Join(p.dataDir, "postmaster.pid"))

	p.expectExit.Store(false)
	p.daemonExit = make(chan struct{})

	cmd := exec.Command(p.binPath("postgres"), "-D", p.dataDir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		log.Error("failed to start postgres", "err", err)
		return err
	}
	p.daemon = cmd
	p.setRunning(true)

	go func() {
		err := cmd.Wait()
		if !p.expectExit.Load().(bool) {
			p.log.Error("postgres unexpectedly exit", "err", err)
			shutdown.ExitWithCode(1)
		}
		close(p.daemonExit)
	}()

	log.Debug("waiting for postgres to start")
	timeout := time.After(p.opTimeout)
	var err error
	for {
		port, _ := strconv.Atoi(p.port)
		c := pgx.ConnPoolConfig{
			ConnConfig: pgx.ConnConfig{
				Host: "127.0.0.1",
				User: "postgres",
				Port: uint16(port),
			},
		}
		p.dbMtx.Lock()
		p.db, err = pgx.NewConnPool(c)
		p.dbMtx.Unlock()
		if err == nil {
			_, err = p.db.Exec("SELECT 1")
			if err == nil {
				log.Info("postgres started")
				return nil
			}
		}

		log.Debug("ignoring error connecting to postgres", "err", err)
		select {
		case <-timeout:
			log.Error("timed out waiting for postgres to start", "err", err)
			if err := p.stop(); err != nil {
				log.Error("error stopping postgres", "err", err)
			}
			return err
		case <-time.After(checkInterval):
		}
	}
}

func (p *Process) stop() error {
	log := p.log.New("fn", "stop")
	log.Info("stopping postgres")

	p.cancelSyncWait()
	p.db.Close()
	p.expectExit.Store(true)

	tryExit := func(sig os.Signal) bool {
		log.Debug("signalling daemon", "sig", sig)
		if err := p.daemon.Process.Signal(sig); err != nil {
			log.Error("error signalling daemon", "sig", sig, "err", err)
		}
		select {
		case <-time.After(p.opTimeout):
			return false
		case <-p.daemonExit:
			p.setRunning(false)
			return true
		}
	}

	// Forcefully disconnect all clients and shut down cleanly
	if tryExit(syscall.SIGINT) {
		return nil
	}

	// Quit immediately, will result in recovery at next start
	if tryExit(syscall.SIGQUIT) {
		return nil
	}

	// If all else fails, forcibly kill the process
	if tryExit(syscall.SIGKILL) {
		return nil
	}

	return errors.New("unable to kill postgres")
}

func (p *Process) sighup() error {
	p.log.Debug("reloading daemon configuration", "fn", "sighup")
	return p.daemon.Process.Signal(syscall.SIGHUP)
}

func (p *Process) waitForSync(inst *discoverd.Instance, config *state.Config, enableWrites bool) {
	stopCh := make(chan struct{})
	doneCh := make(chan struct{})

	var cancelOnce sync.Once
	p.cancelSyncWait = func() {
		cancelOnce.Do(func() {
			close(stopCh)
			<-doneCh
		})
	}
	go func() {
		defer close(doneCh)

		startTime := time.Now().UTC()
		replTimeout := time.NewTimer(p.replTimeout)
		defer replTimeout.Stop()
		lastFlushed := p.XLog().Zero()
		log := p.log.New(
			"fn", "waitForSync",
			"sync_name", inst.Meta[IDKey],
			"start_time", log15.Lazy{func() time.Time { return startTime }},
			"last_flushed", log15.Lazy{func() xlog.Position { return lastFlushed }},
		)

		shouldStop := func() bool {
			select {
			case <-stopCh:
				log.Debug("canceled, stopping")
				return true
			default:
				return false
			}
		}

		sleep := func() bool {
			select {
			case <-stopCh:
				log.Debug("canceled, stopping")
				return false
			case <-time.After(checkInterval):
				return true
			}
		}

		log.Info("waiting for downstream replication to catch up")

		for {
			if shouldStop() {
				return
			}

			sent, flushed, err := p.checkReplStatus(inst.Meta[IDKey])
			if err != nil {
				// If we can't query the replication state, we just keep trying.
				// We do not count this as part of the replication timeout.
				// Generally this means the standby hasn't started or is unable
				// to start. This means that the standby will eventually time
				// itself out and we will exit the loop since a new event will
				// be emitted when the standby leaves the cluster.
				startTime = time.Now().UTC()
				replTimeout.Reset(p.replTimeout)
				if !sleep() {
					return
				}
				continue
			}
			log := log.New("sent", sent, "flushed", flushed, "elapsed", time.Now().Sub(startTime))

			if cmp, err := p.XLog().Compare(lastFlushed, flushed); err != nil {
				log.Error("error parsing log locations", "err", err)
				return
			} else if lastFlushed == p.XLog().Zero() || cmp == -1 {
				log.Debug("flushed row incremented, resetting timeout")
				startTime = time.Now().UTC()
				replTimeout.Reset(p.replTimeout)
				lastFlushed = flushed
			}

			if sent == flushed {
				log.Info("downstream caught up")
				p.setSyncedDownstream(inst)
				break
			}

			select {
			case <-replTimeout.C:
				log.Error("error checking replication status", "err", "downstream unable to make forward progress")
				return
			default:
			}

			log.Debug("continuing replication check")
			if !sleep() {
				return
			}
		}

		if enableWrites {
			// sync caught up, enable write transactions
			if err := p.writeConfig(configData{Sync: inst.Meta[IDKey], Tunables: config.Tunables.Data}); err != nil {
				log.Error("error writing postgres.conf", "err", err)
				return
			}

			if err := p.sighup(); err != nil {
				log.Error("error calling sighup", "err", err)
				return
			}
		}
	}()
}

var ErrNoReplicationStatus = errors.New("no replication status")

func (p *Process) checkReplStatus(name string) (sent, flushed xlog.Position, err error) {
	log := p.log.New("fn", "checkReplStatus", "name", name)
	log.Debug("checking replication status")

	var s, f pgx.NullString
	err = p.db.QueryRow(`
SELECT sent_location, flush_location
FROM pg_stat_replication
WHERE application_name = $1`, name).Scan(&s, &f)
	if err != nil && err != pgx.ErrNoRows {
		log.Error("error checking replication status", "err", err)
		return
	}
	sent, flushed = xlog.Position(s.String), xlog.Position(f.String)
	if err == pgx.ErrNoRows || sent == "" || flushed == "" {
		err = ErrNoReplicationStatus
		log.Debug("no replication status")
		return
	}
	log.Debug("got replication status", "sent_location", sent, "flush_location", flushed)
	return
}

// initDB initializes the postgres data directory for a new dDB. This can fail
// if the db has already been initialized, which is not a fatal error.
//
// This method should only be called by the primary of a shard. Standbys will
// not need to initialize, as they will restore from an already running primary.
func (p *Process) initDB() error {
	log := p.log.New("fn", "initDB", "dir", p.dataDir)
	log.Debug("starting initDB")

	// ignore errors, since the db could be already initialized
	// TODO(titanous): check errors when this is not the case
	_ = p.runCmd(exec.Command(
		p.binPath("initdb"),
		"--pgdata", p.dataDir,
		"--username=postgres",
		"--encoding=UTF-8",
		"--locale=en_US.UTF-8",
	))

	return p.writeHBAConf()
}
func (p *Process) runCmd(cmd *exec.Cmd) error {
	p.log.Debug("running command", "fn", "runCmd", "cmd", cmd.Args)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (p *Process) writeConfig(d configData) error {
	d.ID = p.id
	d.Port = p.port
	d.ExtWhitelist = p.extWhitelist
	f, err := os.Create(p.configPath())
	if err != nil {
		return err
	}
	defer f.Close()
	return configTemplate.Execute(f, d)
}

func (p *Process) writeRecoveryConf(upstream *discoverd.Instance) error {
	data := recoveryData{
		TriggerFile: p.triggerPath(),
		PrimaryInfo: fmt.Sprintf(
			"host=%s port=%s user=flynn password=%s application_name=%s",
			upstream.Host(), upstream.Port(), p.password, p.id,
		),
	}

	f, err := os.Create(p.recoveryConfPath())
	if err != nil {
		return err
	}
	defer f.Close()
	return recoveryConfTemplate.Execute(f, data)
}

func (p *Process) writeHBAConf() error {
	return ioutil.WriteFile(p.hbaConfPath(), hbaConf, 0644)
}

func (p *Process) configPath() string {
	return p.dataPath("postgresql.conf")
}

func (p *Process) recoveryConfPath() string {
	return p.dataPath("recovery.conf")
}

func (p *Process) hbaConfPath() string {
	return p.dataPath("pg_hba.conf")
}

func (p *Process) triggerPath() string {
	return p.dataPath("promote.trigger")
}

func (p *Process) binPath(file string) string {
	return filepath.Join(p.binDir, file)
}

func (p *Process) dataPath(file string) string {
	return filepath.Join(p.dataDir, file)
}

type configData struct {
	ID           string
	Port         string
	Sync         string
	ReadOnly     bool
	ExtWhitelist bool
	Tunables     map[string]string
}

var configTemplate = template.Must(template.New("postgresql.conf").Parse(`
unix_socket_directories = ''
listen_addresses = '0.0.0.0'
port = {{.Port}}
ssl = off
max_connections = 400
wal_level = hot_standby
fsync = on
synchronous_commit = remote_write
synchronous_standby_names = '{{.Sync}}'
{{if .ReadOnly}}
default_transaction_read_only = on
{{end}}
hot_standby = on
hot_standby_feedback = on
log_destination = 'stderr'
logging_collector = false
log_line_prefix = '{{.ID}} %m '

{{if .ExtWhitelist}}
local_preload_libraries = 'pgextwlist'
extwlist.extensions = 'btree_gin,btree_gist,chkpass,citext,cube,dblink,dict_int,earthdistance,fuzzystrmatch,hstore,intarray,isn,ltree,pg_prewarm,pg_stat_statements,pg_trgm,pgcrypto,pgrouting,pgrowlocks,pgstattuple,plpgsql,plv8,postgis,postgis_topology,postgres_fdw,tablefunc,unaccent,uuid-ossp'
{{end}}

{{ range $key, $value := .Tunables }}
{{ $key }} = {{ $value }}
{{ end }}
`[1:]))

type recoveryData struct {
	PrimaryInfo string
	TriggerFile string
}

var recoveryConfTemplate = template.Must(template.New("recovery.conf").Parse(`
standby_mode = on
primary_conninfo = '{{.PrimaryInfo}}'
trigger_file = '{{.TriggerFile}}'
recovery_target_timeline = 'latest'
`[1:]))

var hbaConf = []byte(`
# TYPE  DATABASE        USER            ADDRESS                 METHOD
host    all             postgres        127.0.0.1/32            trust
host    all             all             127.0.0.1/32            md5
host    all             all             all                     md5
host    replication     flynn           all                     md5
`[1:])
