package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

type register struct {
	clientCmd
	exitStatus   int
	exitSignalCh chan os.Signal
	host *string
}

func (cmd *register) Name() string {
	return "register"
}

func (cmd *register) DefineFlags(fs *flag.FlagSet) {
	cmd.SetRegisterFlags(fs)
}

func (cmd *register) SetRegisterFlags(fs *flag.FlagSet) {
	cmd.host = fs.String("h", "", "Specify a particular host for the service")
}

func (cmd *register) RegisterWithExitHook(name, port string, verbose bool) {
	cmd.exitSignalCh = make(chan os.Signal, 1)
	signal.Notify(cmd.exitSignalCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-cmd.exitSignalCh
		if verbose {
			log.Println("Unregistering service...")
		}
		if *cmd.host == "" {
			cmd.client.Unregister(name, port)
		} else {
			cmd.client.UnregisterWithHost(name, *cmd.host, port)
		}
		os.Exit(cmd.exitStatus)
	}()

	if *cmd.host == "" {
		cmd.client.Register(name, port, nil)
	} else {
		cmd.client.RegisterWithHost(name, *cmd.host, port, nil)
	}
}

func (cmd *register) Run(fs *flag.FlagSet) {
	cmd.InitClient(false)
	cmd.exitStatus = 0

	mapping := strings.SplitN(fs.Arg(0), ":", 2)
	name := mapping[0]
	port := mapping[1]

	cmd.RegisterWithExitHook(name, port, true)

	log.Printf("Registered service '%s' on port %s.", name, port)
	for {
		time.Sleep(time.Second)
	}
}
