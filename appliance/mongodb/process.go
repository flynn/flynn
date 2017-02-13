package mongodb

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"text/template"
	"time"

	mongodbxlog "github.com/flynn/flynn/appliance/mongodb/xlog"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/shutdown"
	"github.com/flynn/flynn/pkg/sirenia/client"
	"github.com/flynn/flynn/pkg/sirenia/state"
	"github.com/flynn/flynn/pkg/sirenia/xlog"
	"gopkg.in/inconshreveable/log15.v2"
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
)

const (
	DefaultHost        = "127.0.0.1"
	DefaultPort        = "27017"
	DefaultBinDir      = "/usr/bin"
	DefaultDataDir     = "/data"
	DefaultPassword    = ""
	DefaultOpTimeout   = 5 * time.Minute
	DefaultReplTimeout = 1 * time.Minute

	BinName    = "mongod"
	ConfigName = "mongod.conf"

	checkInterval = 1000 * time.Millisecond
)

var (
	// ErrRunning is returned when starting an already running process.
	ErrRunning = errors.New("process already running")

	// ErrStopped is returned when stopping an already stopped process.
	ErrStopped = errors.New("process already stopped")

	ErrNoReplicationStatus = errors.New("no replication status")
)

// Process represents a MongoDB process.
type Process struct {
	mtx sync.Mutex

	events chan state.DatabaseEvent

	// Replication configuration
	configValue        atomic.Value // *Config
	configAppliedValue atomic.Value // bool

	securityEnabledValue  atomic.Value // bool
	runningValue          atomic.Value // bool
	syncedDownstreamValue atomic.Value // *discoverd.Instance

	ID          string
	Singleton   bool
	Host        string
	Port        string
	BinDir      string
	DataDir     string
	Password    string
	ServerID    uint32
	OpTimeout   time.Duration
	ReplTimeout time.Duration

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
		Host:        DefaultHost,
		Port:        DefaultPort,
		BinDir:      DefaultBinDir,
		DataDir:     DefaultDataDir,
		Password:    DefaultPassword,
		OpTimeout:   DefaultOpTimeout,
		ReplTimeout: DefaultReplTimeout,
		Logger:      log15.New("app", "mongodb"),

		events:         make(chan state.DatabaseEvent, 1),
		cancelSyncWait: func() {},
	}
	p.runningValue.Store(false)
	p.configValue.Store((*state.Config)(nil))
	p.events <- state.DatabaseEvent{}
	return p
}

func (p *Process) running() bool         { return p.runningValue.Load().(bool) }
func (p *Process) securityEnabled() bool { return p.securityEnabledValue.Load().(bool) }
func (p *Process) configApplied() bool   { return p.configAppliedValue.Load().(bool) }
func (p *Process) config() *state.Config { return p.configValue.Load().(*state.Config) }

func (p *Process) syncedDownstream() *discoverd.Instance {
	if downstream, ok := p.syncedDownstreamValue.Load().(*discoverd.Instance); ok {
		return downstream
	}
	return nil
}

func (p *Process) ConfigPath() string { return filepath.Join(p.DataDir, "mongod.conf") }

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
		p.configAppliedValue.Store(false)
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
			"storage.wiredTiger.engineConfig.cacheSizeGB": "1",
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

func (p *Process) applyTunables(config *state.Config) error {
	logger := p.Logger.New("fn", "applyTunables")

	p.writeConfig(configData{ReplicationEnabled: true})

	logger.Info("restarting database to apply tunables")
	if err := p.stop(); err != nil {
		return err
	}
	if err := p.start(); err != nil {
		return err
	}
	return nil
}

func (p *Process) XLog() xlog.XLog {
	return mongodbxlog.XLog{}
}

func (p *Process) getReplConfig() (*replSetConfig, error) {
	// Connect to local server.
	session, err := p.connectLocal()
	if err != nil {
		return nil, err
	}
	defer session.Close()

	// Retrieve replica set configuration.
	var result struct {
		Config replSetConfig `bson:"config"`
	}
	if session.Run(bson.D{{"replSetGetConfig", 1}}, &result); err != nil {
		return nil, err
	}
	return &result.Config, nil
}

func (p *Process) setReplConfig(config replSetConfig) error {
	session, err := p.connectLocal()
	if err != nil {
		return err
	}
	defer session.Close()

	if session.Run(bson.D{{"replSetReconfig", config}, {"force", true}}, nil); err != nil {
		return err
	}
	// XXX(jpg): Prevent mongodb implosion if a reconfigure comes too soon after this one
	time.Sleep(5 * time.Second)
	return nil
}

func clusterSize(clusterState *state.State) int {
	if clusterState.Singleton {
		return 1
	}
	return 2 + len(clusterState.Async)
}

func newMember(addr string, newState *state.State, curIds map[string]int, prio int) replSetMember {
	maxId := clusterSize(newState)
	var id int
	// Keep previous ID if assigned, required for replSetReconfig
	if i, ok := curIds[addr]; ok {
		id = i
	} else {
		// Otherwise assign IDs starting from 0, skipping those in use.
		for i := 0; i < maxId; i++ {
			found := false
			for _, id := range curIds {
				if i == id {
					found = true
				}
			}
			if !found {
				id = i
				break
			}
		}
	}
	curIds[addr] = id // Reserve our newly allocated ID
	return replSetMember{ID: id, Host: addr, Priority: prio}
}

func clusterAddrs(clusterState *state.State) []string {
	addrs := []string{clusterState.Primary.Addr}
	if clusterState.Singleton {
		return addrs
	}
	addrs = append(addrs, clusterState.Sync.Addr)
	for _, n := range clusterState.Async {
		addrs = append(addrs, n.Addr)
	}
	return addrs
}

func (p *Process) replSetConfigFromState(current *replSetConfig, s *state.State) replSetConfig {
	curIds := make(map[string]int, len(current.Members))
	newAddrs := clusterAddrs(s)
	// If any of the current peers are in the new config then preserve their IDs
	for _, m := range current.Members {
		for _, a := range newAddrs {
			if m.Host == a {
				curIds[m.Host] = m.ID
				break
			}
		}
	}
	members := make([]replSetMember, 0, clusterSize(s))
	members = append(members, newMember(s.Primary.Addr, s, curIds, 1))
	// If we aren't running in singleton mode add the other members.
	if !s.Singleton {
		members = append(members, newMember(s.Sync.Addr, s, curIds, 0))
	}
	for _, peer := range s.Async {
		members = append(members, newMember(peer.Addr, s, curIds, 0))
	}
	return replSetConfig{
		ID:      "rs0",
		Members: members,
		Version: current.Version + 1,
	}
}

func (p *Process) reconfigure(config *state.Config) error {
	logger := p.Logger.New("fn", "reconfigure")

	if err := func() error {
		if config != nil && config.Role == state.RoleNone {
			logger.Info("nothing to do", "reason", "null role")
			return nil
		}

		// If we've already applied the same config, we don't need to do anything
		if p.configApplied() && config != nil && p.config() != nil && config.Equal(p.config()) && config.State.Equal(p.config().State) {
			logger.Info("nothing to do", "reason", "config already applied")
			return nil
		}

		// If only tunables have been updated apply them and return.
		if p.running() && p.config().IsTunablesUpdate(config) {
			logger.Info("tunables only update")
			return p.applyTunables(config)
		}

		// If we're already running and it's just a change from async to sync with the same node, we don't need to restart
		if p.configApplied() && p.running() && p.config() != nil && config != nil &&
			p.config().Role == state.RoleAsync && config.Role == state.RoleSync && config.Upstream.Meta["MONGODB_ID"] == p.config().Upstream.Meta["MONGODB_ID"] {
			logger.Info("nothing to do", "reason", "becoming sync with same upstream")
			return nil
		}
		// Make sure that we don't keep waiting for replication sync while reconfiguring
		p.cancelSyncWait()
		p.syncedDownstreamValue.Store((*discoverd.Instance)(nil))

		if config == nil {
			config = p.config()
		}

		if config.Role == state.RolePrimary {
			return p.assumePrimary(config.Downstream, config.State)
		}

		return p.assumeStandby(config.Upstream, config.Downstream)
	}(); err != nil {
		return err
	}

	// Apply configuration.
	p.configValue.Store(config)
	p.configAppliedValue.Store(true)

	return nil
}

func (p *Process) assumePrimary(downstream *discoverd.Instance, clusterState *state.State) (err error) {
	logger := p.Logger.New("fn", "assumePrimary")
	if downstream != nil {
		logger = logger.New("downstream", downstream.Addr)
	}

	if p.running() {
		if p.config().Role == state.RoleSync {
			logger.Info("promoting to primary")
		}
		logger.Info("updating replica set configuration")
		replSetCurrent, err := p.getReplConfig()
		if err != nil {
			return err
		}
		replSetNew := p.replSetConfigFromState(replSetCurrent, clusterState)
		if err := p.setReplConfig(replSetNew); err != nil {
			return err
		}
		p.waitForSync(downstream)
		return nil
	}

	logger.Info("starting as primary")

	// Assert that the process is not running. This should not occur.
	if p.running() {
		panic(fmt.Sprintf("unexpected state running role=%s", p.config().Role))
	}

	// Begin with both replication and security disabled
	// We will enable both once we either initialise the database or confirm
	// that it has already been initialized.
	p.securityEnabledValue.Store(false)
	if err := p.writeConfig(configData{}); err != nil {
		logger.Error("error writing config", "path", p.ConfigPath(), "err", err)
		return err
	}

	if err := p.start(); err != nil {
		return err
	}

	if err := p.initPrimaryDB(clusterState); err != nil {
		logger.Error("error initialising primary, attempting stop")
		if e := p.stop(); err != nil {
			logger.Debug("ignoring error stopping process", "err", e)
		}
		return err
	}

	if downstream != nil {
		p.waitForSync(downstream)
	}

	return nil
}

func (p *Process) assumeStandby(upstream, downstream *discoverd.Instance) error {
	logger := p.Logger.New("fn", "assumeStandby", "upstream", upstream.Addr)

	if p.running() && !p.securityEnabled() {
		logger.Info("stopping database")
		if err := p.stop(); err != nil {
			return err
		}

	}
	p.securityEnabledValue.Store(true)
	if err := p.writeConfig(configData{ReplicationEnabled: true}); err != nil {
		logger.Error("error writing config", "path", p.ConfigPath(), "err", err)
		return err
	}
	logger.Info("starting up as standby")

	if !p.running() {
		logger.Info("starting database")
		if err := p.start(); err != nil {
			return err
		}
	}

	if downstream != nil {
		p.waitForSync(downstream)
	}

	return nil
}

func (p *Process) replSetGetStatus() (*replSetStatus, error) {
	session, err := p.connectLocal()
	if err != nil {
		return nil, err
	}
	defer session.Close()

	return replSetGetStatusQuery(session)
}

func replSetGetStatusQuery(session *mgo.Session) (*replSetStatus, error) {
	var status replSetStatus
	err := session.DB("admin").Run(bson.D{{"replSetGetStatus", 1}}, &status)
	return &status, err
}

func (p *Process) isReplInitialised() (bool, error) {
	_, err := p.replSetGetStatus()
	if err != nil {
		if merr, ok := err.(*mgo.QueryError); ok {
			switch merr.Code {
			case 93: // replica set exists but is invalid/we aren't a member
				return true, nil
			case 94: // replica set not yet configured
				return false, nil
			}
			p.Logger.Error("failed to check if replset initialized", "err", err, "code", merr.Code)
			return false, err
		}
		return false, err
	}
	return true, nil
}

func (p *Process) isUserCreated() (bool, error) {
	session, err := mgo.DialWithInfo(p.DialInfo())
	if err != nil {
		return false, err
	}
	defer session.Close()

	session.SetMode(mgo.Monotonic, true)

	n, err := session.DB("admin").C("system.users").Find(bson.M{"user": "flynn"}).Count()
	if err != nil {
		if merr, ok := err.(*mgo.QueryError); ok && merr.Code == 13 {
			return false, nil
		}
		return false, err
	}
	return n > 0, nil
}

func (p *Process) createUser() error {
	// create a new session
	session, err := mgo.DialWithInfo(p.DialInfo())
	if err != nil {
		return err
	}
	defer session.Close()

	session.SetMode(mgo.Monotonic, true)

	if err := session.DB("admin").Run(bson.D{
		{"createUser", "flynn"},
		{"pwd", p.Password},
		{"roles", []bson.M{{"role": "root", "db": "admin"}, {"role": "dbOwner", "db": "admin"}}},
	}, nil); err != nil {
		return err
	}

	if err := session.DB("admin").Run(bson.D{{"fsync", 1}}, nil); err != nil {
		return err
	}

	return nil
}

// initPrimaryDB initializes the local database with the correct users and plugins.
func (p *Process) initPrimaryDB(clusterState *state.State) error {
	logger := p.Logger.New("fn", "initPrimaryDB")
	logger.Info("initializing primary database")

	// check if admin user has been created
	logger.Info("checking if user has been created")
	created, err := p.isUserCreated()
	if err != nil {
		logger.Error("error checking if user created")
		return err
	}

	// user doesn't exist yet
	if !created {
		logger.Info("stopping database to disable security")
		if err := p.stop(); err != nil {
			return err
		}
		// we need to start the database with both replication and security disabled
		p.securityEnabledValue.Store(false)
		if err := p.writeConfig(configData{}); err != nil {
			logger.Error("error writing config", "path", p.ConfigPath(), "err", err)
			return err
		}
		logger.Info("starting database to create user")
		if err := p.start(); err != nil {
			return err
		}
		logger.Info("creating user")
		if err := p.createUser(); err != nil {
			return err
		}
	}
	logger.Info("stopping database to enable security/replication")
	if err := p.stop(); err != nil {
		return err
	}
	p.securityEnabledValue.Store(true)
	if err := p.writeConfig(configData{ReplicationEnabled: true}); err != nil {
		logger.Error("error writing config", "path", p.ConfigPath(), "err", err)
		return err
	}
	logger.Info("starting database with security enabled")
	if err := p.start(); err != nil {
		return err
	}

	// check if replica set has been initialised
	logger.Info("checking if replica set has been initialised")
	initialized, err := p.isReplInitialised()
	if err != nil {
		logger.Error("error checking replset initialised", "err", err)
		return err
	}
	logger.Info("not initialized, initialising now")
	if !initialized && clusterState != nil {
		if err := p.replSetInitiate(); err != nil {
			logger.Error("error initialising replset", "err", err)
			return err
		}

	}
	logger.Info("getting current replset config")
	replSetCurrent, err := p.getReplConfig()
	if err != nil {
		logger.Error("error getting replset config", "err", err)
		return err
	}

	logger.Info("reconfiguring replset")
	replSetNew := p.replSetConfigFromState(replSetCurrent, clusterState)
	err = p.setReplConfig(replSetNew)
	if err != nil {
		logger.Error("failed to reconfigure replia set", "err", err)
		return err
	}
	return nil
}

func (p *Process) replSetInitiate() error {
	logger := p.Logger.New("fn", "replSetInitiate")
	logger.Info("initialising replica set")
	session, err := p.connectLocal()
	if err != nil {
		return err
	}
	defer session.Close()

	logger.Info("initialising replica set")
	err = session.Run(bson.M{
		"replSetInitiate": replSetConfig{
			ID:      "rs0",
			Members: []replSetMember{{ID: 0, Host: p.addr(), Priority: 1}},
			Version: 1,
		},
	}, nil)
	if err != nil {
		logger.Error("failed to initialise replica set", "err", err)
		return err
	}
	return nil
}

func (p *Process) addr() string {
	return net.JoinHostPort(p.Host, p.Port)
}

func (p *Process) connectLocal() (*mgo.Session, error) {
	session, err := mgo.DialWithInfo(p.DialInfo())
	if err != nil {
		return nil, err
	}
	session.SetMode(mgo.Eventual, true)
	return session, nil
}

func (p *Process) start() error {
	logger := p.Logger.New("fn", "start", "id", p.ID, "port", p.Port)
	logger.Info("starting process")

	cmd := NewCmd(exec.Command(filepath.Join(p.BinDir, "mongod"), "--config", p.ConfigPath()))
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
			session, err := mgo.DialWithInfo(p.DialInfo())
			if err != nil {
				return err
			}
			defer session.Close()

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
				logger.Debug("ignoring error connecting to mongodb", "err", err)
				time.Sleep(checkInterval)
				continue
			}
		}

		logger.Debug("process started")
		return nil
	}
}

func (p *Process) stop() error {
	logger := p.Logger.New("fn", "stop")
	logger.Info("stopping mongodb")

	p.cancelSyncWait()

	logger.Info("attempting graceful shutdown")
	session, err := p.connectLocal()
	if err == nil {
		err := session.DB("admin").Run(bson.D{{"shutdown", 1}, {"force", true}}, nil)
		if err == nil || err == io.EOF {
			select {
			case <-time.After(p.OpTimeout):
				logger.Error("timed out waiting for graceful shutdown, proceeding to kill")
			case <-p.cmd.Stopped():
				logger.Info("database gracefully shutdown")
				p.runningValue.Store(false)
				return nil
			}
		} else {
			logger.Error("error running shutdown command", "err", err)
		}
	} else {
		logger.Error("error connecting to mongodb", "err", err)
	}

	// Attempt to kill.
	logger.Debug("stopping daemon forcefully")
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
	logger := p.Logger.New("fn", "Info")
	info := &client.DatabaseInfo{
		Config:           p.config(),
		Running:          p.running(),
		SyncedDownstream: p.syncedDownstream(),
	}
	xlog, err := p.XLogPosition()
	info.XLog = string(xlog)
	if err != nil {
		logger.Error("error getting xlog")
		return info, err
	}
	info.UserExists, err = p.userExists()
	if err != nil {
		logger.Error("error checking userExists")
		return info, err
	}
	info.ReadWrite, err = p.isReadWrite()
	if err != nil {
		logger.Error("error checking isReadWrite")
		return info, err
	}
	return info, err
}

func (p *Process) isReadWrite() (bool, error) {
	if !p.running() {
		return false, nil
	}
	status, err := p.replSetGetStatus()
	return status.MyState == Primary, err
}

func (p *Process) userExists() (bool, error) {
	if !p.running() {
		return false, errors.New("mongod is not running")
	}

	session, err := p.connectLocal()
	if err != nil {
		return false, err
	}
	defer session.Close()

	type user struct {
		ID       string `bson:"_id"`
		User     string `bson:"user"`
		Database string `bson:"db"`
	}

	var userInfo struct {
		Users []user `bson:"users"`
		Ok    int    `bson:"ok"`
	}

	if err := session.DB("admin").Run(bson.D{{"usersInfo", bson.M{"user": "flynn", "db": "admin"}}}, &userInfo); err != nil {
		return false, err
	}

	for _, u := range userInfo.Users {
		if u.User == "flynn" && u.Database == "admin" {
			return true, nil
		}
	}

	return false, nil
}

func (p *Process) waitForSyncInner(downstream *discoverd.Instance, stopCh, doneCh chan struct{}) {
	defer close(doneCh)

	startTime := time.Now().UTC()
	logger := p.Logger.New(
		"fn", "waitForSync",
		"sync_name", downstream.Meta["MONGODB_ID"],
		"start_time", log15.Lazy{func() time.Time { return startTime }},
	)

	logger.Info("waiting for downstream replication to catch up")
	defer logger.Info("finished waiting for downstream replication")

	for {
		logger.Debug("checking downstream sync")

		// Check if "wait sync" has been canceled.
		select {
		case <-stopCh:
			logger.Debug("canceled, stopping")
			return
		default:
		}

		// get repl status
		status, err := p.replSetGetStatus()
		if err != nil {
			logger.Error("error getting replSetStatus")
			startTime = time.Now().UTC()
			select {
			case <-stopCh:
				logger.Debug("canceled, stopping")
				return
			case <-time.After(checkInterval):
			}
			continue
		}

		var synced bool
		for _, m := range status.Members {
			if m.Name == downstream.Addr && m.State == Secondary {
				synced = true
			}
		}

		if synced {
			p.syncedDownstreamValue.Store(downstream)
			break
		}
		elapsedTime := time.Since(startTime)

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

}

// waitForSync waits for downstream sync in goroutine
func (p *Process) waitForSync(downstream *discoverd.Instance) {
	p.Logger.Debug("waiting for downstream sync")

	stopCh := make(chan struct{})
	doneCh := make(chan struct{})

	var once sync.Once
	p.cancelSyncWait = func() {
		once.Do(func() { close(stopCh); <-doneCh })
	}

	go p.waitForSyncInner(downstream, stopCh, doneCh)
}

// DialInfo returns dial info for connecting to the local process as the "flynn" user.
func (p *Process) DialInfo() *mgo.DialInfo {
	localhost := net.JoinHostPort("localhost", p.Port)
	info := &mgo.DialInfo{
		Addrs:   []string{localhost},
		Timeout: 5 * time.Second,
		Direct:  true,
	}

	if p.securityEnabled() {
		info.Addrs = []string{p.addr()}
		info.Database = "admin"
		info.Username = "flynn"
		info.Password = p.Password
	}
	return info
}

func (p *Process) XLogPosition() (xlog.Position, error) {
	status, err := p.replSetGetStatus()
	if err != nil {
		return p.XLog().Zero(), nil
	}
	return p.xlogPosFromStatus(p.addr(), status)
}

func (p *Process) xlogPosFromStatus(member string, status *replSetStatus) (xlog.Position, error) {
	for _, m := range status.Members {
		if m.Name == member {
			return xlog.Position(strconv.FormatInt(m.Optime.Timestamp, 10)), nil
		}
	}
	return p.XLog().Zero(), fmt.Errorf("error getting xlog, couldn't find member in replSetStatus")
}

func (p *Process) writeConfig(d configData) error {
	d.ID = p.ID
	d.Port = p.Port
	d.DataDir = p.DataDir
	d.SecurityEnabled = p.securityEnabled()
	d.CacheSize = p.config().Tunables.Data["storage.wiredTiger.engineConfig.cacheSizeGB"]
	f, err := os.Create(p.ConfigPath())
	if err != nil {
		return err
	}
	defer f.Close()

	return configTemplate.Execute(f, d)
}

type configData struct {
	ID                 string
	Port               string
	DataDir            string
	CacheSize          string
	SecurityEnabled    bool
	ReplicationEnabled bool
}

// TODO(jpg): Render config from datastructure rather than template
var configTemplate = template.Must(template.New("mongod.conf").Parse(`
storage:
  dbPath: {{.DataDir}}
  journal:
    enabled: true
  engine: wiredTiger
  wiredTiger:
    engineConfig:
      cacheSizeGB: {{.CacheSize}}
net:
  port: {{.Port}}

{{if .SecurityEnabled}}
security:
  keyFile: {{.DataDir}}/Keyfile
  authorization: enabled
{{end}}

{{if .ReplicationEnabled}}
replication:
  replSetName: rs0
  enableMajorityReadConcern: true
{{end}}
`[1:]))
