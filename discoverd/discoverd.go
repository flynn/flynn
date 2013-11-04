package main

import (
	"log"
	"flag"
	"github.com/flynn/go-discover/discover"
)

var addr = flag.String("bind", ":1111", "address to bind on")

func main() {
	flag.Parse()
	server := discover.NewServer(*addr)
	log.Printf("Starting server on %s...\n", server.Address)
	log.Fatal(discover.ListenAndServe(server))
}
