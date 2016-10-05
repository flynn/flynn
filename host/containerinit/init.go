package containerinit

// This package is originally from a fork of Docker and contains no code
// developed by Docker, Inc.
//
// HEAD commit: d0525987c0f29c77520d36a8eec16759e208f64a
// https://github.com/alexlarsson/docker/tree/long-running-dockerinit (original branch)
// https://github.com/titanous/docker/tree/long-running-dockerinit (mirror)
//
// The original code was written by:
//
// Josh Poimboeuf <jpoimboe@redhat.com>
// Alexander Larsson <alexl@redhat.com>
//
// The code is released under the Apache 2.0 license.

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path"
	"path/filepath"
	"runtime/pprof"
	"sync"
	"syscall"
	"time"

	sigutil "github.com/docker/docker/pkg/signal"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/discoverd/health"
	"github.com/flynn/flynn/host/resource"
	"github.com/flynn/flynn/host/types"
	hh "github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/rpcplus"
	"github.com/flynn/flynn/pkg/rpcplus/fdrpc"
	"github.com/kr/pty"
	"gopkg.in/inconshreveable/log15.v2"
)

var logger log15.Logger

type Config struct {
	User      string
	Gateway   string
	WorkDir   string
	IP        string
	TTY       bool
	OpenStdin bool
	Env       map[string]string
	Args      []string
	Ports     []host.Port
	Resources resource.Resources
}

const SharedPath = "/.container-shared"

type State byte

const (
	StateInitial State = iota
	StateRunning
	StateExited
	StateFailed
)

func (s State) String() string {
	switch s {
	case StateInitial:
		return "initial"
	case StateRunning:
		return "running"
	case StateExited:
		return "exited"
	case StateFailed:
		return "failed"
	default:
		return "unknown"
	}
}

type Client struct {
	c *rpcplus.Client
}

func NewClient(path string) (*Client, error) {
	c, err := fdrpc.Dial(path)
	return &Client{c}, err
}

func (c *Client) Close() {
	c.c.Close()
}

type StateChange struct {
	State      State
	Error      string
	ExitStatus int
}

func (c *Client) StreamState() <-chan *StateChange {
	// use a buffered channel otherwise the rpc client will block sending a
	// reply if the client is in the process of making another rpc call in
	// response to a state change and not receiving on the channel
	ch := make(chan *StateChange, 3)

	c.c.StreamGo("ContainerInit.StreamState", struct{}{}, ch)
	return ch
}

func (c *Client) GetState() (State, error) {
	var state State
	return state, c.c.Call("ContainerInit.GetState", struct{}{}, &state)
}

func (c *Client) Resume() error {
	return c.c.Call("ContainerInit.Resume", struct{}{}, &struct{}{})
}

func (c *Client) GetPtyMaster() (*os.File, error) {
	var fd fdrpc.FD
	if err := c.c.Call("ContainerInit.GetPtyMaster", struct{}{}, &fd); err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd.FD), "ptyMaster"), nil
}

func (c *Client) GetStreams() (*os.File, *os.File, *os.File, error) {
	var fds []fdrpc.FD
	if err := c.c.Call("ContainerInit.GetStreams", struct{}{}, &fds); err != nil {
		return nil, nil, nil, err
	}
	if len(fds) != 3 {
		return nil, nil, nil, fmt.Errorf("containerinit: got %d fds, expected 3", len(fds))
	}
	newFile := func(i int, name string) *os.File {
		return os.NewFile(uintptr(fds[i].FD), name)
	}
	return newFile(0, "stdout"), newFile(1, "stderr"), newFile(2, "initLog"), nil
}

func (c *Client) GetStdin() (*os.File, error) {
	var fd fdrpc.FD
	if err := c.c.Call("ContainerInit.GetStdin", struct{}{}, &fd); err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd.FD), "stdin"), nil
}

func (c *Client) Signal(signal int) error {
	err := c.c.Call("ContainerInit.Signal", signal, &struct{}{})
	if err != nil {
		log.Println("Client.Signal", err)
	}
	return err
}

func newContainerInit(c *Config, logFile *os.File) *ContainerInit {
	return &ContainerInit{
		resume:    make(chan struct{}),
		streams:   make(map[chan StateChange]struct{}),
		openStdin: c.OpenStdin,
		logFile:   logFile,
	}
}

type ContainerInit struct {
	mtx        sync.Mutex
	state      State
	resume     chan struct{}
	exitStatus int
	error      string
	process    *os.Process
	stdin      *os.File
	stdout     *os.File
	stderr     *os.File
	logFile    *os.File
	ptyMaster  *os.File
	openStdin  bool

	streams    map[chan StateChange]struct{}
	streamsMtx sync.RWMutex
}

func (c *ContainerInit) GetState(arg *struct{}, status *State) error {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	*status = c.state
	return nil
}

func (c *ContainerInit) Resume(arg, res *struct{}) error {
	c.resume <- struct{}{}
	return nil
}

func (c *ContainerInit) Signal(sig int, res *struct{}) error {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	if err := c.process.Signal(syscall.Signal(sig)); err != nil {
		return err
	}
	return nil
}

func (c *ContainerInit) GetPtyMaster(arg struct{}, fd *fdrpc.FD) error {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	if c.ptyMaster == nil {
		return errors.New("no pty in this container")
	}
	fd.FD = int(c.ptyMaster.Fd())

	return nil
}

func (c *ContainerInit) GetStreams(arg struct{}, fds *[]fdrpc.FD) error {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	*fds = []fdrpc.FD{
		{int(c.stdout.Fd())},
		{int(c.stderr.Fd())},
		{int(c.logFile.Fd())},
	}
	return nil
}

func (c *ContainerInit) GetStdin(arg struct{}, f *os.File) error {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	if c.stdin == nil {
		return errors.New("stdin is closed")
	}

	*f = *c.stdin
	c.stdin = nil

	return nil
}

func (c *ContainerInit) StreamState(arg struct{}, stream rpcplus.Stream) error {
	log := logger.New("fn", "StreamState")
	log.Info("starting to stream state")

	ch := make(chan StateChange)
	c.streamsMtx.Lock()
	c.mtx.Lock()
	select {
	case stream.Send <- StateChange{State: c.state, Error: c.error, ExitStatus: c.exitStatus}:
		log.Info("sent initial state")
	case <-stream.Error:
		c.mtx.Unlock()
		c.streamsMtx.Unlock()
		return nil
	}
	c.mtx.Unlock()
	c.streams[ch] = struct{}{}
	c.streamsMtx.Unlock()
	defer func() {
		log.Info("cleanup")
		go func() {
			// drain to prevent deadlock while removing the listener
			for range ch {
			}
		}()
		c.streamsMtx.Lock()
		delete(c.streams, ch)
		c.streamsMtx.Unlock()
		close(ch)
	}()

	log.Info("waiting for state changes")
	for {
		select {
		case change := <-ch:
			select {
			case stream.Send <- change:
				log.Info("sent state change", "state", change.State)
			case <-stream.Error:
				return nil
			}
		case <-stream.Error:
			return nil
		}
	}
}

// Caller must hold lock
func (c *ContainerInit) changeState(state State, err string, exitStatus int) {
	if err != "" {
		logger.Info("changing state", "fn", "changeState", "state", state, "err", err)
	} else if exitStatus != -1 {
		logger.Info("changing state", "fn", "changeState", "state", state, "exitStatus", exitStatus)
	} else {
		logger.Info("changing state", "fn", "changeState", "state", state)
	}

	c.state = state
	c.error = err
	c.exitStatus = exitStatus

	c.streamsMtx.RLock()
	defer c.streamsMtx.RUnlock()
	for ch := range c.streams {
		ch <- StateChange{State: state, Error: err, ExitStatus: exitStatus}
	}
}

func (c *ContainerInit) exit(status int) {
	// Wait for the client to call Resume() again. This gives the client a
	// chance to get the exit code from the RPC socket call interface
	// before we die.
	select {
	case <-c.resume:
	case <-time.After(time.Second):
		log.Println("timeout waiting for client to call Resume()")
	}
	os.Exit(status)
}

var SocketPath = filepath.Join(SharedPath, "rpc.sock")

func runRPCServer() {
	os.Remove(SocketPath)
	logger.Info("starting RPC server", "fn", "runRPCServer")
	fdrpc.ListenAndServe(SocketPath)
	os.Exit(70)
}

func getCmdPath(c *Config) (string, error) {
	// Set PATH in containerinit so we can find the cmd
	if envPath := c.Env["PATH"]; envPath != "" {
		os.Setenv("PATH", envPath)
	}

	// Find the cmd
	cmdPath, err := exec.LookPath(c.Args[0])
	if err != nil {
		if c.WorkDir == "" {
			return "", err
		}
		if cmdPath, err = exec.LookPath(path.Join(c.WorkDir, c.Args[0])); err != nil {
			return "", err
		}
	}

	return cmdPath, nil
}

func monitor(port host.Port, container *ContainerInit, env map[string]string, log log15.Logger) (discoverd.Heartbeater, error) {
	config := port.Service
	client := discoverd.NewClientWithURL(env["DISCOVERD"])
	client.Logger = logger.New("component", "discoverd")

	if config.Create {
		// TODO: maybe reuse maybeAddService() from the client
		log.Info("creating service")
		if err := client.AddService(config.Name, nil); err != nil {
			if !hh.IsObjectExistsError(err) {
				log.Error("error creating service", "err", err)
				return nil, fmt.Errorf("something went wrong with discoverd: %s", err)
			}
		}
	}
	inst := &discoverd.Instance{
		Addr:  fmt.Sprintf("%s:%v", env["EXTERNAL_IP"], port.Port),
		Proto: port.Proto,
	}
	// add discoverd.EnvInstanceMeta if present
	for k, v := range env {
		if _, ok := discoverd.EnvInstanceMeta[k]; !ok {
			continue
		}
		if inst.Meta == nil {
			inst.Meta = make(map[string]string)
		}
		inst.Meta[k] = v
	}

	// no checker, but we still want to register a service
	if config.Check == nil {
		log.Info("registering instance", "instance", inst)
		return client.RegisterInstance(config.Name, inst)
	}

	var check health.Check
	switch config.Check.Type {
	case "tcp":
		check = &health.TCPCheck{Addr: inst.Addr}
	case "http", "https":
		check = &health.HTTPCheck{
			URL:        fmt.Sprintf("%s://%s%s", config.Check.Type, inst.Addr, config.Check.Path),
			Host:       config.Check.Host,
			StatusCode: config.Check.Status,
			MatchBytes: []byte(config.Check.Match),
		}
	default:
		// unsupported checker type
		return nil, fmt.Errorf("unsupported check type: %s", config.Check.Type)
	}
	log.Info("adding healthcheck", "type", config.Check.Type, "interval", config.Check.Interval, "threshold", config.Check.Threshold)
	reg := health.Registration{
		Registrar: client,
		Service:   config.Name,
		Instance:  inst,
		Monitor: health.Monitor{
			Interval:  config.Check.Interval,
			Threshold: config.Check.Threshold,
			Logger:    log.New("component", "monitor"),
		}.Run,
		Check:  check,
		Logger: log,
	}

	if config.Check.KillDown {
		reg.Events = make(chan health.MonitorEvent)
		go func() {
			if config.Check.StartTimeout == 0 {
				config.Check.StartTimeout = 10 * time.Second
			}

			start := false
			lastStatus := health.MonitorStatusDown
			var mtx sync.Mutex

			maybeKill := func() {
				if lastStatus == health.MonitorStatusDown {
					log.Warn("killing the job")
					container.Signal(int(syscall.SIGKILL), &struct{}{})
				}
			}
			go func() {
				// ignore events for the first StartTimeout interval
				<-time.After(config.Check.StartTimeout)
				mtx.Lock()
				defer mtx.Unlock()
				maybeKill() // check if the app is down
				start = true
			}()

			for e := range reg.Events {
				log.Info("got health monitor event", "status", e.Status)
				mtx.Lock()
				lastStatus = e.Status
				if !start {
					mtx.Unlock()
					continue
				}
				maybeKill()
				mtx.Unlock()
			}
		}()
	}
	return reg.Register(), nil
}

func babySit(process *os.Process, hbs []discoverd.Heartbeater) int {
	log := logger.New("fn", "babySit")

	var shutdownOnce sync.Once
	hbDone := make(chan struct{})
	closeHBs := func() {
		for _, hb := range hbs {
			if err := hb.Close(); err != nil {
				log.Error("error closing heartbeater", "err", err)
			} else {
				log.Info("closed heartbeater")
			}
		}
		close(hbDone)
	}

	// Forward all signals to the app
	sigchan := make(chan os.Signal, 1)
	sigutil.CatchAll(sigchan)
	go func() {
		for sig := range sigchan {
			log.Info("received signal", "type", sig)
			if sig == syscall.SIGCHLD {
				continue
			}
			if sig == syscall.SIGTERM || sig == syscall.SIGINT {
				shutdownOnce.Do(closeHBs)
				time.Sleep(2 * time.Second)
			}
			log.Info("forwarding signal to command", "type", sig)
			process.Signal(sig)
		}
	}()

	// Wait for the app to exit.  Also, as pid 1 it's our job to reap all
	// orphaned zombies.
	var wstatus syscall.WaitStatus
	for {
		pid, err := syscall.Wait4(-1, &wstatus, 0, nil)
		if err == nil && pid == process.Pid {
			break
		}
	}

	// Ensure that the heartbeaters are closed even if the app wasn't signaled
	shutdownOnce.Do(closeHBs)
	select {
	case <-hbDone:
	case <-time.After(5 * time.Second):
		log.Error("timed out waiting for heartbeaters to close")
	}

	if wstatus.Signaled() {
		log.Info("command exited due to signal")
		return 0
	}

	return wstatus.ExitStatus()
}

// Run as pid 1 and monitor the contained process to return its exit code.
func containerInitApp(c *Config, logFile *os.File) error {
	log := logger.New("fn", "containerInitApp")

	init := newContainerInit(c, logFile)
	log.Info("registering RPC server")
	if err := rpcplus.Register(init); err != nil {
		log.Error("error registering RPC server", "err", err)
		return err
	}
	init.mtx.Lock()
	defer init.mtx.Unlock()

	// Prepare the cmd based on the given args
	// If this fails we report that below
	cmdPath, cmdErr := getCmdPath(c)
	cmd := exec.Command(cmdPath, c.Args[1:]...)
	cmd.Dir = c.WorkDir

	cmd.Env = make([]string, 0, len(c.Env))
	for k, v := range c.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	// App runs in its own session
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	// Console setup.  Hook up the container app's stdin/stdout/stderr to
	// either a pty or pipes.  The FDs for the controlling side of the
	// pty/pipes will be passed to flynn-host later via a UNIX socket.
	if c.TTY {
		log.Info("creating PTY")
		ptyMaster, ptySlave, err := pty.Open()
		if err != nil {
			log.Info("error creating PTY", "err", err)
			return err
		}
		init.ptyMaster = ptyMaster
		cmd.Stdout = ptySlave
		cmd.Stderr = ptySlave
		if c.OpenStdin {
			log.Info("attaching stdin to PTY")
			cmd.Stdin = ptySlave
			cmd.SysProcAttr.Setctty = true
		}
	} else {
		// we use syscall.Socketpair (rather than cmd.StdoutPipe) to make it easier
		// for flynn-host to do non-blocking I/O (via net.FileConn) so that no
		// read(2) calls can succeed after closing the logs during an update.
		log.Info("creating stdout pipe")
		var err error
		cmd.Stdout, init.stdout, err = newSocketPair("stdout")
		if err != nil {
			log.Error("error creating stdout pipe", "err", err)
			return err
		}

		log.Info("creating stderr pipe")
		cmd.Stderr, init.stderr, err = newSocketPair("stderr")
		if err != nil {
			log.Error("error creating stderr pipe", "err", err)
			return err
		}

		log.Info("creating FD proxies")
		if err := createFDProxies(cmd); err != nil {
			log.Error("error creating FD proxies", "err", err)
			return err
		}

		if c.OpenStdin {
			// Can't use cmd.StdinPipe() here, since in Go 1.2 it
			// returns an io.WriteCloser with the underlying object
			// being an *exec.closeOnce, neither of which provides
			// a way to convert to an FD.
			log.Info("creating stdin pipe")
			pipeRead, pipeWrite, err := os.Pipe()
			if err != nil {
				log.Error("creating stdin pipe", "err", err)
				return err
			}
			cmd.Stdin = pipeRead
			init.stdin = pipeWrite
		}
	}

	go runRPCServer()

	// Wait for flynn-host to tell us to start
	init.mtx.Unlock() // Allow calls
	log.Info("waiting to be resumed")
	<-init.resume
	log.Info("resuming")
	init.mtx.Lock()

	if cmdErr != nil {
		log.Error("command failed", "err", cmdErr)
		init.changeState(StateFailed, cmdErr.Error(), -1)
		init.exit(1)
	}
	// Start the app
	log.Info("starting the command")
	if err := cmd.Start(); err != nil {
		log.Error("error starting the command", "err", err)
		init.changeState(StateFailed, err.Error(), -1)
		init.exit(1)
	}
	log.Info("setting state to running")
	init.process = cmd.Process
	init.changeState(StateRunning, "", -1)

	init.mtx.Unlock() // Allow calls
	// monitor services
	hbs := make([]discoverd.Heartbeater, 0, len(c.Ports))
	for _, port := range c.Ports {
		if port.Service == nil {
			continue
		}
		log = log.New("service", port.Service.Name, "port", port.Port, "proto", port.Proto)
		log.Info("monitoring service")
		hb, err := monitor(port, init, c.Env, log)
		if err != nil {
			log.Error("error monitoring service", "err", err)
			os.Exit(70)
		}
		hbs = append(hbs, hb)
	}
	exitCode := babySit(init.process, hbs)
	log.Info("command exited", "status", exitCode)
	init.mtx.Lock()
	init.changeState(StateExited, "", exitCode)
	init.mtx.Unlock() // Allow calls

	log.Info("exiting")
	init.exit(exitCode)
	return nil
}

func newSocketPair(name string) (*os.File, *os.File, error) {
	pair, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
	if err != nil {
		return nil, nil, err
	}
	return os.NewFile(uintptr(pair[0]), name), os.NewFile(uintptr(pair[1]), name), nil
}

// createFDProxies creates pipes at /dev/stdout and /dev/stderr and copies data
// written to them to the job's stdout and stderr streams respectively.
//
// This is necessary (rather than just symlinking those paths to /proc/self/fd/{1,2})
// because the standard streams are sockets, and calling open(2) on a socket
// leads to an ENXIO error (see http://marc.info/?l=ast-users&m=120978595414993).
func createFDProxies(cmd *exec.Cmd) error {
	for path, dst := range map[string]*os.File{
		"/dev/stdout": cmd.Stdout.(*os.File),
		"/dev/stderr": cmd.Stderr.(*os.File),
	} {
		os.Remove(path)
		if err := syscall.Mkfifo(path, 0666); err != nil {
			return err
		}
		pipe, err := os.OpenFile(path, os.O_RDWR, os.ModeNamedPipe)
		if err != nil {
			return err
		}
		go func(dst *os.File) {
			defer pipe.Close()
			for {
				// copy data from the pipe to dst using splice(2) (rather than io.Copy)
				// to avoid a needless copy through user space
				n, err := syscall.Splice(int(pipe.Fd()), nil, int(dst.Fd()), nil, 65535, 0)
				if err != nil || n == 0 {
					return
				}
			}
		}(dst)
	}
	return nil
}

// print a full goroutine stack trace to the log fd on SIGUSR2
func debugStackPrinter(out io.Writer) {
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGUSR2)
	for range c {
		pprof.Lookup("goroutine").WriteTo(out, 1)
	}
}

// This code is run INSIDE the container and is responsible for setting
// up the environment before running the actual process
func Main() {
	logR, logW, err := newSocketPair("log")
	if err != nil {
		os.Exit(70)
	}
	go debugStackPrinter(logW)

	logger = log15.New("app", "containerinit")
	logger.SetHandler(log15.StreamHandler(logW, log15.LogfmtFormat()))

	config := &Config{}
	data, err := ioutil.ReadFile("/.containerconfig")
	if err != nil {
		os.Exit(70)
	}
	if err := json.Unmarshal(data, config); err != nil {
		os.Exit(70)
	}

	// Propagate the plugin-specific container env variable
	config.Env["container"] = os.Getenv("container")

	if err := containerInitApp(config, logR); err != nil {
		os.Exit(70)
	}
}
