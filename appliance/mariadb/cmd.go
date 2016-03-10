package mariadb

import (
	"os"
	"os/exec"
	"sync/atomic"
	"syscall"
)

// Cmd wraps exec.Cmd and provides helpers for checking for expected exits.
type Cmd struct {
	*exec.Cmd
	stoppingValue atomic.Value // bool
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
