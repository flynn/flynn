package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/pkg/shutdown"
)

var (
	// ControlMsgResume is sent via a control socket from a parent daemon
	// to its child to request that the child start serving requests.
	ControlMsgResume = []byte{0}

	// ControlMsgOK is sent via a control socket from a child daemon to
	// its parent to indicate it has received a resume request and is now
	// serving requests.
	ControlMsgOK = []byte{1}
)

// Update performs a zero-downtime update of the flynn-host daemon, replacing
// the current daemon with an instance of the given command.
//
// The HTTP API listener is passed from the parent to the child, but due to the
// state DBs being process exclusive and requiring initialisation, further
// syncronisation is required to manage opening and closing them, which is done
// using a control socket.
//
// Any partial log lines read by the parent are also passed to the child to
// avoid dropping logs or sending partial logs over two lines.
//
// An outline of the process:
//
// * parent receives a request to exec a new daemon
//
// * parent creates a control socket pair (via socketpair(2))
//
// * parent starts a child process, passing the API listener as FD 3, and a
//   control socket as FD 4
//
// * parent closes its API listener FD, state DBs and log followers.
//
// * parent signals the child to resume by sending "resume" message to control
//   socket, followed by any partial log buffers.
//
// * child receives resume request, opens state DBs, seeds the log followers
//   with the partial buffers and starts serving API requests
//
// * child signals parent it is now serving requests by sending "ok" message to
//   control socket
//
// * parent sends response to client and shuts down seconds later
//
func (h *Host) Update(cmd *host.Command) error {
	log := h.log.New("fn", "Update")

	// dup the listener so we can close the current listener but still be
	// able continue serving requests if the child exits by using the dup'd
	// listener.
	log.Info("duplicating HTTP listener")
	file, err := h.listener.(interface {
		File() (*os.File, error)
	}).File()
	if err != nil {
		log.Error("error duplicating HTTP listener", "err", err)
		return err
	}
	defer file.Close()

	// exec a child, passing the listener and control socket as extra files
	log.Info("creating child process")
	child, err := h.exec(cmd, file)
	if err != nil {
		log.Error("error creating child process", "err", err)
		return err
	}
	defer child.CloseSock()

	// close our listener and state DBs
	log.Info("closing HTTP listener")
	h.listener.Close()
	log.Info("closing state databases")
	if err := h.CloseDBs(); err != nil {
		log.Error("error closing state databases", "err", err)
		return err
	}

	log.Info("closing logs")
	buffers, err := h.CloseLogs()
	if err != nil {
		log.Error("error closing logs", "err", err)
		return err
	}

	log.Info("resuming child process")
	if resumeErr := child.Resume(buffers); resumeErr != nil {
		log.Error("error resuming child process", "err", resumeErr)

		// The child failed to resume, kill it and resume ourselves.
		//
		// If anything fails here, exit rather than returning an error
		// so a new host process can be started (rather than this
		// process sitting around not serving requests).
		log.Info("killing child process")
		child.Kill()

		log.Info("reopening logs")
		if err := h.OpenLogs(buffers); err != nil {
			shutdown.Fatalf("error reopening logs after failed update: %s", err)
		}

		log.Error("recreating HTTP listener")
		l, err := net.FileListener(file)
		if err != nil {
			shutdown.Fatalf("error recreating HTTP listener after failed update: %s", err)
		}
		h.listener = l

		log.Info("reopening state databases")
		if err := h.OpenDBs(); err != nil {
			shutdown.Fatalf("error reopening state databases after failed update: %s", err)
		}

		log.Info("serving HTTP requests")
		h.ServeHTTP()

		return resumeErr
	}

	return nil
}

func (h *Host) exec(cmd *host.Command, listener *os.File) (*Child, error) {
	// create a control socket for communicating with the child
	sockPair, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
	if err != nil {
		return nil, fmt.Errorf("error creating socketpair: %s", err)
	}

	dir, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("error getting working directory: %s", err)
	}

	h.statusMtx.RLock()
	status, err := json.Marshal(h.status)
	h.statusMtx.RUnlock()
	if err != nil {
		return nil, fmt.Errorf("error marshaling status JSON: %s", err)
	}

	c := exec.Command(cmd.Path, cmd.Args...)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	c.Dir = dir
	c.ExtraFiles = []*os.File{
		listener,
		os.NewFile(uintptr(sockPair[1]), "child"),
	}
	setEnv(c, map[string]string{
		"FLYNN_HTTP_FD":     "3",
		"FLYNN_CONTROL_FD":  "4",
		"FLYNN_HOST_STATUS": string(status),
	})
	if err := c.Start(); err != nil {
		return nil, fmt.Errorf("error exec'ing child: %s", err)
	}
	cmd.PID = c.Process.Pid
	syscall.Close(sockPair[1])
	return &Child{c, sockPair[0]}, nil
}

// setEnv sets the given environment variables for the command, ensuring they
// are only set once.
func setEnv(cmd *exec.Cmd, envs map[string]string) {
	env := os.Environ()
	cmd.Env = make([]string, 0, len(env)+len(envs))
outer:
	for _, e := range env {
		for k := range envs {
			if strings.HasPrefix(e, k+"=") {
				continue outer
			}
		}
		cmd.Env = append(cmd.Env, e)
	}
	for k, v := range envs {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
}

type Child struct {
	cmd  *exec.Cmd
	sock int
}

// controlSock is a wrapper around an fd returned from socketpair(2)
type controlSock struct {
	fd int
}

func (s *controlSock) Read(p []byte) (int, error) {
	return syscall.Read(s.fd, p)
}

// Resume writes ControlMsgResume to the control socket and waits for a
// ControlMsgOK response
func (c *Child) Resume(buffers host.LogBuffers) error {
	if buffers == nil {
		buffers = host.LogBuffers{}
	}
	data, err := json.Marshal(buffers)
	if err != nil {
		return err
	}
	if _, err := syscall.Write(c.sock, append(ControlMsgResume, data...)); err != nil {
		return err
	}
	msg := make([]byte, len(ControlMsgOK))
	if _, err := syscall.Read(c.sock, msg); err != nil {
		return err
	}
	if !bytes.Equal(msg, ControlMsgOK) {
		return fmt.Errorf("unexpected resume message from child: %s", msg)
	}
	return nil
}

func (c *Child) CloseSock() error {
	return syscall.Close(c.sock)
}

func (c *Child) Kill() error {
	if err := c.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		return c.cmd.Process.Kill()
	}
	done := make(chan struct{})
	go func() {
		c.cmd.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-time.After(10 * time.Second):
		return c.cmd.Process.Kill()
	}
}
