package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/flynn/flynn/discoverd/client"
)

const service = "signal-service"

func main() {
	ch := make(chan os.Signal)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	if err := discoverd.Register(service, ":12345"); err != nil {
		log.Fatal(err)
	}
	sig := <-ch
	fmt.Printf("got signal: %s", sig)
}
