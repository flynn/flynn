package main

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/progrium/go-discover/discover"
)

func main() {
	flag.Parse()
	name := os.Args[0]
	port := os.Args[1]
	host := os.Args[2]

	exit := make(chan os.Signal, 1)
	signal.Notify(exit, os.Interrupt, syscall.SIGTERM)
	var cleanup func()
	go func() {
		<-exit
		if cleanup != nil {
			cleanup()
		}
		os.Exit(0)
	}()

	client, _ := discover.NewClient()
	if host != "" {
		client.RegisterWithHost(name, host, port, nil)
		cleanup = func() { client.UnregisterWithHost(name, host, port) }
	} else {
		client.Register(name, port, nil)
		cleanup = func() { client.Unregister(name, port) }
	}
	fmt.Println("Registered %s on port %s.\n", name, port)

	set := client.Services(name)
	for {
		fmt.Println(strings.Join(set.OnlineAddrs(), ", "))
		time.Sleep(1 * time.Second)
	}
}
