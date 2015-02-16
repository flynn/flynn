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
	"io/ioutil"
	"log"
	"net"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	sigutil "github.com/flynn/flynn/Godeps/_workspace/src/github.com/docker/docker/pkg/signal"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/docker/libcontainer/netlink"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/docker/libcontainer/user"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/kr/pty"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/discoverd/health"
	"github.com/flynn/flynn/host/types"
	hh "github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/rpcplus"
	"github.com/flynn/flynn/pkg/rpcplus/fdrpc"
	"github.com/flynn/flynn/pkg/shutdown"
)

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
	ch := make(chan *StateChange)
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

func (c *Client) GetStdout() (*os.File, *os.File, error) {
	var fds []fdrpc.FD
	if err := c.c.Call("ContainerInit.GetStdout", struct{}{}, &fds); err != nil {
		return nil, nil, err
	}
	if len(fds) != 2 {
		return nil, nil, fmt.Errorf("containerinit: got %d fds, expected 2", len(fds))
	}
	return os.NewFile(uintptr(fds[0].FD), "stdout"), os.NewFile(uintptr(fds[1].FD), "stderr"), nil
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

func newContainerInit(c *Config) *ContainerInit {
	return &ContainerInit{
		resume:    make(chan struct{}),
		streams:   make(map[chan StateChange]struct{}),
		openStdin: c.OpenStdin,
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

// Get the exit code (or -1 if running)
func (c *ContainerInit) GetExitStatus(arg *struct{}, status *int) error {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	*status = c.exitStatus
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

func (c *ContainerInit) GetStdout(arg struct{}, fds *[]fdrpc.FD) error {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	*fds = []fdrpc.FD{{int(c.stdout.Fd())}, {int(c.stderr.Fd())}}
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
	ch := make(chan StateChange)
	c.streamsMtx.Lock()
	c.mtx.Lock()
	select {
	case stream.Send <- StateChange{State: c.state, Error: c.error, ExitStatus: c.exitStatus}:
	case <-stream.Error:
		c.mtx.Unlock()
		c.streamsMtx.Unlock()
		return nil
	}
	c.mtx.Unlock()
	c.streams[ch] = struct{}{}
	c.streamsMtx.Unlock()
	defer func() {
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

	for {
		select {
		case change := <-ch:
			select {
			case stream.Send <- change:
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
	shutdown.Fatal(fdrpc.ListenAndServe(SocketPath))
}

func setupHostname(c *Config) error {
	hostname := c.Env["HOSTNAME"]
	if hostname == "" {
		return nil
	}
	return syscall.Sethostname([]byte(hostname))
}

func setupNetworking(c *Config) error {
	if c.IP == "" {
		return nil
	}

	// loopback
	iface, err := net.InterfaceByName("lo")
	if err != nil {
		return fmt.Errorf("Unable to set up networking: %v", err)
	}
	if err := netlink.NetworkLinkUp(iface); err != nil {
		return fmt.Errorf("Unable to set up networking: %v", err)
	}
	if iface, err = net.InterfaceByName("eth0"); err != nil {
		return fmt.Errorf("Unable to set up networking: %v", err)
	}
	ip, ipNet, err := net.ParseCIDR(c.IP)
	if err != nil {
		return fmt.Errorf("Unable to set up networking: %v", err)
	}
	if err := netlink.NetworkLinkAddIp(iface, ip, ipNet); err != nil {
		return fmt.Errorf("Unable to set up networking: %v", err)
	}
	if c.Gateway != "" {
		if err := netlink.AddDefaultGw(c.Gateway, "eth0"); err != nil {
			return fmt.Errorf("Unable to set up networking: %v", err)
		}
	}
	if err := netlink.NetworkLinkUp(iface); err != nil {
		return fmt.Errorf("Unable to set up networking: %v", err)
	}

	return nil
}

func getCredential(c *Config) (*syscall.Credential, error) {
	if c.User == "" {
		return nil, nil
	}
	users, err := user.ParsePasswdFileFilter("/etc/passwd", func(u user.User) bool {
		return u.Name == c.User
	})
	if err != nil || len(users) == 0 {
		if err == nil {
			err = errors.New("unknown user")
		}
		return nil, fmt.Errorf("Unable to find user %v: %v", c.User, err)
	}

	return &syscall.Credential{Uid: uint32(users[0].Uid), Gid: uint32(users[0].Gid)}, nil
}

func setupCommon(c *Config) error {
	if err := setupHostname(c); err != nil {
		return err
	}

	if err := setupNetworking(c); err != nil {
		return err
	}

	return nil
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

func monitor(port host.Port, container *ContainerInit, env map[string]string) (discoverd.Heartbeater, error) {
	config := port.Service
	client := discoverd.NewClientWithURL(env["DISCOVERD"])

	if config.Create {
		// TODO: maybe reuse maybeAddService() from the client
		if err := client.AddService(config.Name); err != nil {
			if je, ok := err.(hh.JSONError); !ok || je.Code != hh.ObjectExistsError {
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
	reg := health.Registration{
		Registrar: client,
		Service:   config.Name,
		Instance:  inst,
		Monitor: health.Monitor{
			Interval:  config.Check.Interval,
			Threshold: config.Check.Threshold,
		}.Run,
		Check: check,
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

func babySit(process *os.Process) int {
	// Forward all signals to the app
	sigchan := make(chan os.Signal, 1)
	sigutil.CatchAll(sigchan)
	go func() {
		for sig := range sigchan {
			if sig == syscall.SIGCHLD {
				continue
			}
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

	if wstatus.Signaled() {
		return 0
	}
	return wstatus.ExitStatus()
}

// Run as pid 1 and monitor the contained process to return its exit code.
func containerInitApp(c *Config) error {
	init := newContainerInit(c)
	if err := rpcplus.Register(init); err != nil {
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
		ptyMaster, ptySlave, err := pty.Open()
		if err != nil {
			return err
		}
		init.ptyMaster = ptyMaster
		cmd.Stdout = ptySlave
		cmd.Stderr = ptySlave
		if c.OpenStdin {
			cmd.Stdin = ptySlave
			cmd.SysProcAttr.Setctty = true
		}
	} else {
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return err
		}
		init.stdout = stdout.(*os.File)

		stderr, err := cmd.StderrPipe()
		if err != nil {
			return err
		}
		init.stderr = stderr.(*os.File)
		if c.OpenStdin {
			// Can't use cmd.StdinPipe() here, since in Go 1.2 it
			// returns an io.WriteCloser with the underlying object
			// being an *exec.closeOnce, neither of which provides
			// a way to convert to an FD.
			pipeRead, pipeWrite, err := os.Pipe()
			if err != nil {
				return err
			}
			cmd.Stdin = pipeRead
			init.stdin = pipeWrite
		}
	}

	go runRPCServer()

	// Wait for flynn-host to tell us to start
	init.mtx.Unlock() // Allow calls
	<-init.resume
	init.mtx.Lock()

	if cmdErr != nil {
		init.changeState(StateFailed, cmdErr.Error(), -1)
		init.exit(1)
	}
	// Container setup
	if err := setupCommon(c); err != nil {
		init.changeState(StateFailed, err.Error(), -1)
		init.exit(1)
	}
	// Start the app
	if err := cmd.Start(); err != nil {
		init.changeState(StateFailed, err.Error(), -1)
		init.exit(1)
	}
	init.process = cmd.Process
	init.changeState(StateRunning, "", -1)

	init.mtx.Unlock() // Allow calls
	// monitor services
	hbs := make([]discoverd.Heartbeater, 0, len(c.Ports))
	for _, port := range c.Ports {
		if port.Service == nil {
			continue
		}
		hb, err := monitor(port, init, c.Env)
		if err != nil {
			log.Printf("Error trying to monitor: %v", err)
			shutdown.Fatal(err)
		}
		hbs = append(hbs, hb)
	}
	exitCode := babySit(init.process)
	init.mtx.Lock()
	for _, hb := range hbs {
		hb.Close()
	}
	init.changeState(StateExited, "", exitCode)
	init.mtx.Unlock() // Allow calls

	init.exit(exitCode)
	return nil
}

// This code is run INSIDE the container and is responsible for setting
// up the environment before running the actual process
func Main() {
	config := &Config{}
	data, err := ioutil.ReadFile("/.containerconfig")
	if err != nil {
		log.Fatalf("Unable to load config: %v", err)
	}
	if err := json.Unmarshal(data, config); err != nil {
		log.Fatalf("Unable to unmarshal config: %v", err)
	}

	// Propagate the plugin-specific container env variable
	config.Env["container"] = os.Getenv("container")

	if err := containerInitApp(config); err != nil {
		shutdown.Fatal(err)
	}
}
