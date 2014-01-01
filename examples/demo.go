package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/flynn/go-discoverd"
)

func main() {
	flag.Parse()
	name := flag.Arg(0)
	port := flag.Arg(1)
	host := flag.Arg(2)

	exit := make(chan os.Signal, 1)
	signal.Notify(exit, os.Interrupt, syscall.SIGTERM)
	var cleanup func()
	go func() {
		<-exit
		log.Println("Shutting down...")
		if cleanup != nil {
			cleanup()
		}
		os.Exit(0)
	}()

	client, err := discoverd.NewClient()
	if err != nil {
		log.Fatal("Error making client: ", err.Error())
	}
	if host != "" {
		client.RegisterWithHost(name, host, port, nil)
		cleanup = func() { client.UnregisterWithHost(name, host, port) }
	} else {
		client.Register(name, port, nil)
		cleanup = func() { client.Unregister(name, port) }
	}
	log.Printf("Registered %s on port %s.\n", name, port)

	set := client.Services(name)
	for {
		log.Println(strings.Join(set.OnlineAddrs(), ", "))
		time.Sleep(1 * time.Second)
	}
}
