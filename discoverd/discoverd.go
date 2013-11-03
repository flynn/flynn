package main

import (
	"fmt"
	"flag"
	"github.com/flynn/go-discover/discover"
)

var addr = flag.String("bind", ":1111", "address to bind on")

func main() {
	flag.Parse()
	server := discover.NewServer(*addr)
	fmt.Printf("Starting server on %s...\n", server.Address)
	discover.ListenAndServe(server)
}
