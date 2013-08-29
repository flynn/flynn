package main

import (
	"github.com/progrium/go-discover/discover"
	"fmt"
)

func main() {
	server := discover.NewServer()
	fmt.Printf("Starting server on %s...\n", server.Address)
	discover.ServeForever(server)
}