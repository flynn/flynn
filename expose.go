package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

type dependencySpec struct {
	service, targetVar string
}

// A flag.Value that is a list of dependencies
type depSlice []dependencySpec

func (s *depSlice) String() string {
	return "ENV:service-name, -d=service-name"
}

func (s *depSlice) Set(value string) error {
	parts := strings.SplitN(value, ":", 2)
	var spec dependencySpec
	if len(parts) == 1 {
		spec = dependencySpec{service: parts[0], targetVar: parts[0]}
	} else {
		spec = dependencySpec{service: parts[1], targetVar: parts[0]}
	}
	*s = append(*s, spec)
	return nil
}

type expose struct {
	clientCmd
	dependencies *depSlice
}

func (cmd *expose) Name() string {
	return "expose"
}

func (cmd *expose) DefineFlags(fs *flag.FlagSet) {
	var t depSlice
	cmd.dependencies = &t
	fs.Var(cmd.dependencies, "d", "services to depend on")
}

type dependencyState struct {
	deps map[string]string
}

// Given a request for a set of services needed, trigger updates
// when the request can be fullfilled, when the services chosen
// change, or when it can no longer be fullfilled.
func (cmd *expose) requireDependencies(services *depSlice) (chan dependencyState, error) {
	deps := make(map[string]string)
	status := make(chan dependencyState)
	l := new(sync.Mutex)

	for _, dependency := range *services {
		set, err := cmd.client.NewServiceSet(dependency.service)
		if err != nil {
			return nil, err
		}
		go func(targetVar string) {
			defer set.Close()

			for leader := range set.Leaders() {
				l.Lock()
				if leader != nil {
					fmt.Println("new leader for service:", leader.Addr)
					deps[targetVar] = leader.Addr
					if len(deps) == len(*services) {
						// All services are available, make a copy to
						// pass through the channel.
						depState := dependencyState{deps: make(map[string]string, len(deps))}
						for k, v := range deps {
							depState.deps[k] = v
						}
						status <- depState
					}
				} else if deps[targetVar] != "" {
					fmt.Println("debug: current leader went offline:", deps[targetVar])
					delete(deps, targetVar)
					status <- dependencyState{deps: nil}
				}
				l.Unlock()
			}
		}(dependency.targetVar)
	}

	return status, nil
}

func (cmd *expose) Run(fs *flag.FlagSet) {
	cmd.InitClient(false)

	args := fs.Args()
	if len(args) < 1 {
		fmt.Println("no command to exec")
		os.Exit(1)
		return
	}

	fmt.Println("Discovering services..")
	serviceVarsUpdate, err := cmd.requireDependencies(cmd.dependencies)
	if err != nil {
		fmt.Println(err)
		os.Exit(2)
	}

	var exitCh chan uint
	var proc *exec.Cmd

	for {
		select {
		case state := <-serviceVarsUpdate:
			fmt.Println("debug: state received", state)

			if state.deps == nil && exitCh != nil {
				fmt.Println("Lost a required service, shutting down...")
				proc.Process.Signal(syscall.SIGTERM)
				select {
				case <-exitCh:
					break
				case <-time.After(5 * time.Second):
					fmt.Println("Waiting for shutdown timed out, killing process")
					if err := proc.Process.Kill(); err != nil {
						panic("failed to kill")
					}
					<-exitCh
				}
				fmt.Println("Process ended, waiting for dependencies...")
				proc = nil
				exitCh = nil

			} else if state.deps != nil && exitCh == nil {
				fmt.Println("All services available, starting...")
				proc, exitCh = startCommand(args, state.deps)
			}

		case exitStatus := <-exitCh:
			fmt.Println("debug: program exit code received")
			os.Exit(int(exitStatus))
		}
	}
}

func startCommand(args []string, env map[string]string) (*exec.Cmd, chan uint) {
	c := exec.Command(args[0], args[1:]...)

	c.Env = os.Environ()
	for key, value := range env {
		envitem := fmt.Sprintf("%s=%s", key, value)
		c.Env = append(c.Env, envitem)
	}

	errCh := attachCmd(c, os.Stdout, os.Stderr, os.Stdin)
	err := c.Start()
	if err != nil {
		panic(err)
	}

	go func() {
		if err = <-errCh; err != nil {
			panic(err)
		}
	}()
	return c, exitStatusCh(c)
}
