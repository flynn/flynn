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
	addr := flag.Arg(1)

	client, err := discoverd.NewClient()
	if err != nil {
		log.Fatal("Error making client:", err)
	}
	if err = client.Register(name, addr); err != nil {
		log.Fatal("Error registering:", err)
	}
	log.Printf("Registered %s at %s.\n", name, addr)

	exit := make(chan os.Signal, 1)
	signal.Notify(exit, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-exit
		log.Println("Shutting down...")
		client.Unregister(name, addr)
		os.Exit(0)
	}()

	set, err := client.NewServiceSet(name)
	if err != nil {
		log.Fatal("Error getting ServiceSet:", err)
	}
	for _ = range time.Tick(time.Second) {
		log.Println(strings.Join(set.Addrs(), ", "))
	}
}
