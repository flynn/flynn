package main

import (
	"github.com/progrium/go-discover/discover"
	"fmt"
	"flag"
	"time"
	"strings"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	flag.Parse()
	name := flag.Arg(0)
	port := flag.Arg(1)
	host := flag.Arg(2)
	
	exit := make(chan os.Signal, 1)
	signal.Notify(exit, os.Interrupt)
	signal.Notify(exit, syscall.SIGTERM)
	var cleanup func()
	go func() {
        <-exit
		if cleanup != nil {
			cleanup()
		}
        os.Exit(0)
    }()
	
	client := discover.NewClient()
	if host != "" {
		client.RegisterWithHost(name, host, port, nil)
	    cleanup = func() {	client.UnregisterWithHost(name, host, port) }
	} else {
		client.Register(name, port, nil)
		cleanup = func() {	client.Unregister(name, port) }
	}
	fmt.Printf("Registered %s on port %s.\n", name, port)
	
	set := client.Services(name)
	for {
		fmt.Printf("%s\n", strings.Join(set.OnlineAddrs(), ", "))
		time.Sleep(1 * time.Second)
	}
}