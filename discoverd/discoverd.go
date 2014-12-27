package main

import (
	"flag"
	"log"
	"strings"

	"github.com/flynn/flynn/discoverd/agent"
)

var addr = flag.String("bind", ":1111", "address to bind on")
var etcd = flag.String("etcd", "http://127.0.0.1:2379", "etcd servers")

func main() {
	flag.Parse()
	server := agent.NewServer(*addr, strings.Split(*etcd, ","))
	log.Printf("Starting server on %s...\n", server.Address)
	log.Fatal(agent.ListenAndServe(server))
}
