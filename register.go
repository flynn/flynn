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
}

func (cmd *register) Name() string {
	return "register"
}

func (cmd *register) DefineFlags(fs *flag.FlagSet) {
}

func (cmd *register) Run(fs *flag.FlagSet) {
	cmd.InitClient()
	mapping := strings.SplitN(fs.Arg(0), ":", 2)
	name := mapping[0]
	port := mapping[1]

	exit := make(chan os.Signal, 1)
	signal.Notify(exit, os.Interrupt, syscall.SIGTERM)
	var cleanup func()
	go func() {
		<-exit
		log.Println("Unregistering service...")
		if cleanup != nil {
			cleanup()
		}
		os.Exit(0)
	}()

	cmd.client.Register(name, port, nil)
	cleanup = func() { cmd.client.Unregister(name, port) }
	log.Printf("Registered service '%s' on port %s.", name, port)

	for {
		time.Sleep(1)
	}
}
