package main

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

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/jackc/pgx"
	"github.com/flynn/flynn/Godeps/_workspace/src/gopkg.in/inconshreveable/log15.v2"
	"github.com/flynn/flynn/appliance/postgresql/client"
	"github.com/flynn/flynn/appliance/postgresql/state"
	"github.com/flynn/flynn/appliance/postgresql/xlog"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/shutdown"
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
	SHMType      string
	WaitUpstream bool
}

type Postgres struct {
	// dbMtx is locked while db is being changed
	dbMtx sync.RWMutex
	db    *pgx.ConnPool

	events chan state.PostgresEvent

	configVal     atomic.Value // *state.PgConfig
	runningVal    atomic.Value // bool
	configApplied bool

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
	shmType      string
	waitUpstream bool

	// daemon is the postgres daemon command when running
	daemon *exec.Cmd
	// expectExit is bool, true if the daemon is supposed to exit
	expectExit atomic.Value
	// daemonExit is closed when the daemon exits
	daemonExit chan struct{}

	// cancelSyncWait cancels the goroutine that is waiting for the sync to
	// catch up, if running
	cancelSyncWait func()

	// mtx ensures that only one operation happens at a time
	mtx sync.Mutex
}

const checkInterval = 100 * time.Millisecond

func NewPostgres(c Config) state.Postgres {
	p := &Postgres{
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
		shmType:        c.SHMType,
		waitUpstream:   c.WaitUpstream,
		events:         make(chan state.PostgresEvent, 1),
		cancelSyncWait: func() {},
	}
	p.setRunning(false)
	p.setConfig(nil)
	if p.log == nil {
		p.log = log15.New("app", "postgres", "id", p.id)
	}
	if p.port == "" {
		p.port = "5432"
	}
	if p.binDir == "" {
		p.binDir = "/usr/lib/postgresql/9.3/bin/"
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
	p.events <- state.PostgresEvent{}
	return p
}

func (p *Postgres) running() bool {
	return p.runningVal.Load().(bool)
}

func (p *Postgres) setRunning(running bool) {
	p.runningVal.Store(running)
}

func (p *Postgres) config() *state.PgConfig {
	return p.configVal.Load().(*state.PgConfig)
}

func (p *Postgres) setConfig(config *state.PgConfig) {
	p.configVal.Store(config)
}

func (p *Postgres) Info() (*pgmanager.PostgresInfo, error) {
	res := &pgmanager.PostgresInfo{
		Config:  p.config(),
		Running: p.running(),
	}
	xlog, err := p.XLogPosition()
	res.XLog = string(xlog)
	if err != nil {
		return res, err
	}
	res.Replicas, err = p.getReplicas()
	return res, err
}

func (p *Postgres) Reconfigure(config *state.PgConfig) error {
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

func (p *Postgres) Start() error {
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

func (p *Postgres) Stop() error {
	p.mtx.Lock()
	defer p.mtx.Unlock()

	if !p.running() {
		return errors.New("postgres is already stopped")
	}
	return p.stop()
}

func (p *Postgres) XLogPosition() (xlog.Position, error) {
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

func (p *Postgres) Ready() <-chan state.PostgresEvent {
	return p.events
}

func (p *Postgres) reconfigure(config *state.PgConfig) (err error) {
	defer func() {
		if err == nil {
			p.setConfig(config)
			p.configApplied = true
		}
	}()

	if config != nil && config.Role == state.RoleNone {
		return nil
	}

	// If we've already applied the same postgres config, we don't need to do anything
	if p.configApplied && config != nil && p.config() != nil && config.Equal(p.config()) {
		return nil
	}

	// If we're already running and it's just a change from async to sync with the same node, we don't need to restart
	if p.configApplied && p.running() && p.config() != nil && config != nil &&
		p.config().Role == state.RoleAsync && config.Role == state.RoleSync && config.Upstream.ID == p.config().Upstream.ID {
		return nil
	}

	// Make sure that we don't keep waiting for a sync to come up while reconfiguring
	p.cancelSyncWait()

	// If we're already running and this is only a sync change, we just need to update the config.
	if p.running() && p.config() != nil && config != nil && p.config().Role == state.RolePrimary && config.Role == state.RolePrimary {
		return p.updateSync(config.Downstream)
	}

	if config == nil {
		config = p.config()
	}

	if config.Role == state.RolePrimary {
		return p.assumePrimary(config.Downstream)
	}

	return p.assumeStandby(config.Upstream)
}

func (p *Postgres) assumePrimary(downstream *discoverd.Instance) (err error) {
	log := p.log.New("fn", "assumePrimary")
	if downstream != nil {
		log = log.New("downstream", downstream.Addr)
	}

	if p.running() && p.config().Role == state.RoleSync {
		log.Info("promoting to primary")

		if err := ioutil.WriteFile(p.triggerPath(), nil, 0655); err != nil {
			log.Error("error creating trigger file", "path", p.triggerPath(), "err", err)
			return err
		}

		p.waitForSync(downstream)

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

	if err := p.writeConfig(configData{ReadOnly: downstream != nil}); err != nil {
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
	if _, err := tx.Exec(fmt.Sprintf(`CREATE USER flynn WITH SUPERUSER CREATEDB CREATEROLE REPLICATION PASSWORD '%s'`, p.password)); err != nil {
		if e, ok := err.(pgx.PgError); !ok || e.Code != "42710" { // role already exists
			log.Error("error creating superuser", "err", err)
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		log.Error("error committing transaction", "err", err)
		return err
	}

	if downstream != nil {
		p.waitForSync(downstream)
	}

	return nil
}

func (p *Postgres) assumeStandby(upstream *discoverd.Instance) error {
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
				upstream.Host(), upstream.Port(), p.password, upstream.ID,
			),
			"--xlog-method=stream",
			"--progress",
			"--verbose",
		))
		if err != nil {
			log.Error("error pulling basebackup", "err", err)
			return err
		}
	}

	if err := p.writeConfig(configData{ReadOnly: true}); err != nil {
		log.Error("error writing postgres.conf", "path", p.configPath(), "err", err)
		return err
	}
	if err := p.writeRecoveryConf(upstream); err != nil {
		log.Error("error writing recovery.conf", "path", p.recoveryConfPath(), "err", err)
		return err
	}

	return p.start()
}

func (p *Postgres) waitForUpstream(upstream *discoverd.Instance) error {
	log := p.log.New("fn", "waitForUpstream", "upstream", upstream.Addr)
	log.Info("waiting for upstream to come online")
	client := pgmanager.NewClient(upstream.Addr)

	start := time.Now()
	for {
		status, err := client.Status()
		if err != nil {
			log.Error("error getting upstream status", "err", err)
		} else if status.Postgres.Running && status.Postgres.XLog != "" {
			log.Info("upstream is online")
			return nil
		}
		time.Sleep(checkInterval)
		if time.Now().Sub(start) > p.opTimeout {
			log.Error("upstream did not come online in time")
			return errors.New("upstream is offline")
		}
	}
}

func (p *Postgres) updateSync(downstream *discoverd.Instance) error {
	log := p.log.New("fn", "updateSync", "downstream", downstream.Addr)
	log.Info("changing sync")

	config := configData{
		ReadOnly: true,
		Sync:     downstream.ID,
	}

	if err := p.writeConfig(config); err != nil {
		log.Error("error writing postgres.conf", "path", p.configPath(), "err", err)
		return err
	}

	if err := p.sighup(); err != nil {
		p.log.Error("error reloading daemon configuration", "err", err)
		return err
	}

	p.waitForSync(downstream)

	return nil
}

func (p *Postgres) start() error {
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
	startTime := time.Now().UTC()
	var err error
	for {
		if time.Now().Sub(startTime) > p.opTimeout {
			log.Error("timed out waiting for postgres to start", "err", err)
			if err := p.stop(); err != nil {
				log.Error("error stopping postgres", "err", err)
			}
			return err
		}

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
		time.Sleep(checkInterval)
	}
}

func (p *Postgres) stop() error {
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

func (p *Postgres) sighup() error {
	p.log.Debug("reloading daemon configuration", "fn", "sighup")
	return p.daemon.Process.Signal(syscall.SIGHUP)
}

func (p *Postgres) waitForSync(inst *discoverd.Instance) {
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
		lastFlushed := xlog.Zero
		log := p.log.New(
			"fn", "waitForSync",
			"sync_name", inst.ID,
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

		log.Info("waiting for sync replication to catch up")

		for {
			if shouldStop() {
				return
			}

			sent, flushed, err := p.checkReplStatus(inst.ID)
			if err != nil {
				// If we can't query the replication state, we just keep trying.
				// We do not count this as part of the replication timeout.
				// Generally this means the standby hasn't started or is unable
				// to start. This means that the standby will eventually time
				// itself out and we will exit the loop since a new event will
				// be emitted when the standby leaves the cluster.
				startTime = time.Now().UTC()
				if !sleep() {
					return
				}
				continue
			}
			elapsedTime := time.Now().Sub(startTime)
			log := log.New("sent", sent, "flushed", flushed, "elapsed", elapsedTime)

			if cmp, err := xlog.Compare(lastFlushed, flushed); err != nil {
				log.Error("error parsing log locations", "err", err)
				return
			} else if lastFlushed == xlog.Zero || cmp == -1 {
				log.Debug("flushed row incremented, resetting startTime")
				startTime = time.Now().UTC()
				lastFlushed = flushed
			}

			if sent == flushed {
				log.Info("sync caught up")
				break
			} else if elapsedTime > p.replTimeout {
				log.Error("error checking replication status", "err", "sync unable to make forward progress")
				return
			} else {
				log.Debug("continuing replication check")
				if !sleep() {
					return
				}
				continue
			}
		}

		// sync caught up, enable write transactions
		if err := p.writeConfig(configData{Sync: inst.ID}); err != nil {
			log.Error("error writing postgres.conf", "err", err)
			return
		}

		if err := p.sighup(); err != nil {
			log.Error("error calling sighup")
			return
		}
	}()
}

var ErrNoReplicationStatus = errors.New("no replication status")

func (p *Postgres) checkReplStatus(name string) (sent, flushed xlog.Position, err error) {
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

func (p *Postgres) getReplicas() ([]*pgmanager.Replica, error) {
	p.dbMtx.RLock()
	defer p.dbMtx.RUnlock()
	if !p.running() || p.db == nil {
		return nil, nil
	}

	rows, err := p.db.Query(`
SELECT application_name, client_addr, client_port, backend_start, state, sent_location, 
       write_location, flush_location, replay_location, sync_state
FROM pg_stat_replication`)
	if err == pgx.ErrNoRows {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	var res []*pgmanager.Replica
	for rows.Next() {
		replica, err := scanReplica(rows)
		if err != nil {
			return nil, err
		}
		res = append(res, replica)
	}
	return res, nil
}

func scanReplica(row *pgx.Rows) (*pgmanager.Replica, error) {
	var start pgx.NullTime
	var id, addr, state, sentLoc, writeLoc, flushLoc, replayLoc, sync pgx.NullString
	var port pgx.NullInt32
	if err := row.Scan(&id, &addr, &port, &start, &state, &sentLoc, &writeLoc, &flushLoc, &replayLoc, &sync); err != nil {
		return nil, err
	}
	return &pgmanager.Replica{
		ID:             id.String,
		Addr:           fmt.Sprintf("%s:%d", addr.String, port.Int32),
		Start:          start.Time,
		State:          state.String,
		Sync:           sync.String == "sync",
		SentLocation:   sentLoc.String,
		WriteLocation:  writeLoc.String,
		FlushLocation:  flushLoc.String,
		ReplayLocation: replayLoc.String,
	}, nil
}

// initDB initializes the postgres data directory for a new dDB. This can fail
// if the db has already been initialized, which is not a fatal error.
//
// This method should only be called by the primary of a shard. Standbys will
// not need to initialize, as they will restore from an already running primary.
func (p *Postgres) initDB() error {
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
func (p *Postgres) runCmd(cmd *exec.Cmd) error {
	p.log.Debug("running command", "fn", "runCmd", "cmd", cmd.Args)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (p *Postgres) writeConfig(d configData) error {
	d.ID = p.id
	d.Port = p.port
	d.ExtWhitelist = p.extWhitelist
	d.SHMType = p.shmType
	f, err := os.Create(p.configPath())
	if err != nil {
		return err
	}
	defer f.Close()
	return configTemplate.Execute(f, d)
}

func (p *Postgres) writeRecoveryConf(upstream *discoverd.Instance) error {
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

func (p *Postgres) writeHBAConf() error {
	return ioutil.WriteFile(p.hbaConfPath(), hbaConf, 0644)
}

func (p *Postgres) configPath() string {
	return p.dataPath("postgresql.conf")
}

func (p *Postgres) recoveryConfPath() string {
	return p.dataPath("recovery.conf")
}

func (p *Postgres) hbaConfPath() string {
	return p.dataPath("pg_hba.conf")
}

func (p *Postgres) triggerPath() string {
	return p.dataPath("promote.trigger")
}

func (p *Postgres) binPath(file string) string {
	return filepath.Join(p.binDir, file)
}

func (p *Postgres) dataPath(file string) string {
	return filepath.Join(p.dataDir, file)
}

type configData struct {
	ID       string
	Port     string
	Sync     string
	ReadOnly bool

	ExtWhitelist bool
	SHMType      string
}

var configTemplate = template.Must(template.New("postgresql.conf").Parse(`
unix_socket_directories = ''
listen_addresses = '0.0.0.0'
port = {{.Port}}
ssl = off
ssl_renegotiation_limit = 0 # fix for https://github.com/flynn/flynn/issues/101
max_connections = 100
shared_buffers = 32MB
wal_level = hot_standby
fsync = on
max_wal_senders = 15
wal_keep_segments = 1000
synchronous_commit = remote_write
synchronous_standby_names = '{{.Sync}}'
{{if .ReadOnly}}
default_transaction_read_only = on
{{end}}
hot_standby = on
max_standby_archive_delay = 30s
max_standby_streaming_delay = 30s
wal_receiver_status_interval = 10s
hot_standby_feedback = on
log_destination = 'stderr'
logging_collector = false
log_line_prefix = '{{.ID}} %m '
log_timezone = 'UTC'
log_connections = on
log_disconnections = on
datestyle = 'iso, mdy'
timezone = 'UTC'
client_encoding = 'UTF8'
default_text_search_config = 'pg_catalog.english'

{{if .SHMType}}
dynamic_shared_memory_type = '{{.SHMType}}'
{{end}}

{{if .ExtWhitelist}}
local_preload_libraries = 'pgextwlist'
extwlist.extensions = 'btree_gin,btree_gist,chkpass,citext,cube,dblink,dict_int,earthdistance,fuzzystrmatch,hstore,intarray,isn,ltree,pg_prewarm,pg_stat_statements,pg_trgm,pgcrypto,pgrowlocks,pgstattuple,plpgsql,plv8,postgis,postgis_topology,postgres_fdw,tablefunc,unaccent,uuid-ossp'
{{end}}
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
