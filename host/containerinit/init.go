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
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	sigutil "github.com/flynn/flynn/Godeps/_workspace/src/github.com/docker/docker/pkg/signal"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/docker/libcontainer/netlink"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/docker/libcontainer/user"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/kr/pty"
	"github.com/flynn/flynn/pkg/rpcplus"
	"github.com/flynn/flynn/pkg/rpcplus/fdrpc"
)

type ContainerInitArgs struct {
	user       string
	gateway    string
	workDir    string
	ip         string
	privileged bool
	tty        bool
	openStdin  bool
	child      bool
	env        []string
	args       []string
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

func newContainerInit(args *ContainerInitArgs) *ContainerInit {
	return &ContainerInit{
		resume:    make(chan struct{}),
		streams:   make(map[chan StateChange]struct{}),
		openStdin: args.openStdin,
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

func (c *ContainerInit) GetStdin(arg struct{}, fd *fdrpc.ClosingFD) error {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	if c.stdin == nil {
		return errors.New("stdin is closed")
	}

	fd.FD = int(c.stdin.Fd())
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
			for _ = range ch {
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

var SocketPath = filepath.Join(SharedPath, "rpc.sock")

func runRPCServer() {
	os.Remove(SocketPath)
	log.Fatal(fdrpc.ListenAndServe(SocketPath))
}

func setupHostname(args *ContainerInitArgs) error {
	hostname := getEnv(args, "HOSTNAME")
	if hostname == "" {
		return nil
	}
	return syscall.Sethostname([]byte(hostname))
}

func setupNetworking(args *ContainerInitArgs) error {
	// loopback
	iface, err := net.InterfaceByName("lo")
	if err != nil {
		return fmt.Errorf("Unable to set up networking: %v", err)
	}
	if err := netlink.NetworkLinkUp(iface); err != nil {
		return fmt.Errorf("Unable to set up networking: %v", err)
	}
	if args.ip != "" {
		if iface, err = net.InterfaceByName("eth0"); err != nil {
			return fmt.Errorf("Unable to set up networking: %v", err)
		}
		ip, ipNet, err := net.ParseCIDR(args.ip)
		if err != nil {
			return fmt.Errorf("Unable to set up networking: %v", err)
		}
		if err := netlink.NetworkLinkAddIp(iface, ip, ipNet); err != nil {
			return fmt.Errorf("Unable to set up networking: %v", err)
		}
		if args.gateway != "" {
			if err := netlink.AddDefaultGw(args.gateway, "eth0"); err != nil {
				return fmt.Errorf("Unable to set up networking: %v", err)
			}
		}
		if err := netlink.NetworkLinkUp(iface); err != nil {
			return fmt.Errorf("Unable to set up networking: %v", err)
		}
	}

	return nil
}

func getCredential(args *ContainerInitArgs) (*syscall.Credential, error) {
	if args.user == "" {
		return nil, nil
	}
	users, err := user.ParsePasswdFilter(func(u *user.User) bool {
		return u.Name == args.user
	})
	if err != nil || len(users) == 0 {
		if err == nil {
			err = errors.New("unknown user")
		}
		return nil, fmt.Errorf("Unable to find user %v: %v", args.user, err)
	}

	return &syscall.Credential{Uid: uint32(users[0].Uid), Gid: uint32(users[0].Gid)}, nil
}

func setupCommon(args *ContainerInitArgs) error {
	if err := setupHostname(args); err != nil {
		return err
	}

	if err := setupNetworking(args); err != nil {
		return err
	}

	return nil
}

func getEnv(args *ContainerInitArgs, key string) string {
	for _, kv := range args.env {
		parts := strings.SplitN(kv, "=", 2)
		if parts[0] == key && len(parts) == 2 {
			return parts[1]
		}
	}
	return ""
}

func getCmdPath(args *ContainerInitArgs) (string, error) {
	// Set PATH in containerinit so we can find the cmd
	if envPath := getEnv(args, "PATH"); envPath != "" {
		os.Setenv("PATH", envPath)
	}

	// Find the cmd
	cmdPath, err := exec.LookPath(args.args[0])
	if err != nil {
		if args.workDir == "" {
			return "", err
		}
		if cmdPath, err = exec.LookPath(path.Join(args.workDir, args.args[0])); err != nil {
			return "", err
		}
	}

	return cmdPath, nil
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
func containerInitApp(args *ContainerInitArgs) error {
	init := newContainerInit(args)
	if err := rpcplus.Register(init); err != nil {
		return err
	}
	init.mtx.Lock()
	defer init.mtx.Unlock()

	// Prepare the cmd based on the given args
	// If this fails we report that below
	cmdPath, cmdErr := getCmdPath(args)
	cmd := exec.Command(cmdPath, args.args[1:]...)
	cmd.Dir = args.workDir
	cmd.Env = args.env

	// App runs in its own session
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	// Console setup.  Hook up the container app's stdin/stdout/stderr to
	// either a pty or pipes.  The FDs for the controlling side of the
	// pty/pipes will be passed to flynn-host later via a UNIX socket.
	if args.tty {
		ptyMaster, ptySlave, err := pty.Open()
		if err != nil {
			return err
		}
		init.ptyMaster = ptyMaster
		cmd.Stdout = ptySlave
		cmd.Stderr = ptySlave
		if args.openStdin {
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
		if args.openStdin {
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

	exitCode := 1

	if cmdErr != nil {
		init.changeState(StateFailed, cmdErr.Error(), -1)
		return cmdErr
	}
	// Container setup
	if err := setupCommon(args); err != nil {
		init.changeState(StateFailed, err.Error(), -1)
	}
	// Start the app
	if err := cmd.Start(); err != nil {
		init.changeState(StateFailed, err.Error(), -1)
	}
	init.process = cmd.Process
	init.changeState(StateRunning, "", -1)

	init.mtx.Unlock() // Allow calls
	exitCode = babySit(init.process)
	init.mtx.Lock()
	init.changeState(StateExited, "", exitCode)

	init.mtx.Unlock() // Allow calls

	// Wait for the client to call Resume() again. This gives the client
	// a chance to get the exit code from the RPC socket call interface before
	// we die.
	select {
	case <-init.resume:
	case <-time.After(time.Second):
		return fmt.Errorf("timeout waiting for client to call Resume()")
	}

	init.mtx.Lock()

	os.Exit(exitCode)
	return nil
}

// This code is run INSIDE the container and is responsible for setting
// up the environment before running the actual process
func Main() {
	if len(os.Args) <= 1 {
		fmt.Println("You should not invoke containerinit manually")
		os.Exit(1)
	}

	// Get cmdline arguments
	user := flag.String("u", "", "username or uid")
	gateway := flag.String("g", "", "gateway address")
	workDir := flag.String("w", "", "workdir")
	ip := flag.String("i", "", "ip address")
	privileged := flag.Bool("privileged", false, "privileged mode")
	tty := flag.Bool("tty", false, "use pseudo-tty")
	openStdin := flag.Bool("stdin", false, "open stdin")
	flag.Parse()

	// Get env
	var env []string
	content, err := ioutil.ReadFile("/.containerenv")
	if err != nil {
		log.Fatalf("Unable to load environment variables: %v", err)
	}
	if err := json.Unmarshal(content, &env); err != nil {
		log.Fatalf("Unable to unmarshal environment variables: %v", err)
	}

	// Propagate the plugin-specific container env variable
	env = append(env, "container="+os.Getenv("container"))

	args := &ContainerInitArgs{
		user:       *user,
		gateway:    *gateway,
		workDir:    *workDir,
		ip:         *ip,
		privileged: *privileged,
		tty:        *tty,
		openStdin:  *openStdin,
		env:        env,
		args:       flag.Args(),
	}

	if err := containerInitApp(args); err != nil {
		log.Fatal(err)
	}
}
