package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// A flag.Value that is a map of service registrations
type regSlice map[string]string

func (s *regSlice) String() string {
	return "name:port"
}

func (s *regSlice) Set(value string) error {
	colonIdx := strings.LastIndex(value, ":")
	if colonIdx == -1 {
		fmt.Println("specify services in name:port format:", value)
		os.Exit(1)
	}
	(*s)[value[0:colonIdx]] = value[colonIdx+1:]
	return nil
}

type execCmd struct {
	register
	services *regSlice
}

func (cmd *execCmd) Name() string {
	return "exec"
}

func (cmd *execCmd) DefineFlags(fs *flag.FlagSet) {
	cmd.SetRegisterFlags(fs)
	t := make(regSlice)
	cmd.services = &t
	fs.Var(cmd.services, "s", "services to register")
}

func (cmd *execCmd) Run(fs *flag.FlagSet) {
	cmd.InitClient(false)
	cmd.exitStatus = 0

	cmd.ValidateFlags()

	args := fs.Args()
	if len(args) < 1 {
		fmt.Println("no command to exec")
		os.Exit(1)
		return
	}
	var c *exec.Cmd
	if len(args) > 2 {
		c = exec.Command(args[0], args[1:]...)
	} else {
		c = exec.Command(args[0])
	}
	errCh := attachCmd(c, os.Stdout, os.Stderr, os.Stdin)
	err := c.Start()
	if err != nil {
		panic(err)
	}

	cmd.RegisterWithExitHook(map[string]string(*cmd.services), false)

	exitCh := exitStatusCh(c)
	if err = <-errCh; err != nil {
		panic(err)
	}
	cmd.exitStatus = int(<-exitCh)
	close(cmd.exitSignalCh)
	time.Sleep(time.Second)
}

func attachCmd(cmd *exec.Cmd, stdout, stderr io.Writer, stdin io.Reader) chan error {
	errCh := make(chan error)

	stdinIn, err := cmd.StdinPipe()
	if err != nil {
		panic(err)
	}
	stdoutOut, err := cmd.StdoutPipe()
	if err != nil {
		panic(err)
	}
	stderrOut, err := cmd.StderrPipe()
	if err != nil {
		panic(err)
	}

	go func() {
		_, e := io.Copy(stdinIn, stdin)
		errCh <- e
	}()
	go func() {
		_, e := io.Copy(stdout, stdoutOut)
		errCh <- e
	}()
	go func() {
		_, e := io.Copy(stderr, stderrOut)
		errCh <- e
	}()

	return errCh
}

func exitStatusCh(cmd *exec.Cmd) chan uint {
	exitCh := make(chan uint, 1)
	go func() {
		err := cmd.Wait()
		if err != nil {
			if exiterr, ok := err.(*exec.ExitError); ok {
				// There is no plattform independent way to retrieve
				// the exit code, but the following will work on Unix
				if status, ok := exiterr.Sys().(syscall.WaitStatus); ok {
					exitCh <- uint(status.ExitStatus())
				}
			} else {
				panic(err)
			}
			return
		}
		exitCh <- uint(0)
	}()
	return exitCh
}
