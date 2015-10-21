package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/flynn/flynn/discoverd/client"
)

func main() {
	log.SetFlags(log.Lmicroseconds)

	log.Println("printing first half")
	fmt.Printf("hello ")

	ch := make(chan os.Signal, 1)
	log.Println("setting signal handler")
	signal.Notify(ch, syscall.SIGUSR1)

	log.Println("registering service")
	hb, err := discoverd.AddServiceAndRegister("partial-logger", ":12345")
	if err != nil {
		log.Fatal(err)
	}
	defer hb.Close()

	log.Println("waiting for signal")
	<-ch

	log.Println("printing second half")
	fmt.Printf("world\n")
}
