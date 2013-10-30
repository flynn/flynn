package main

import (
	"log"
	"os"

	"github.com/flynn/rpcplus"
	"github.com/flynn/strowger/types"
)

func main() {
	client, err := rpcplus.DialHTTP("tcp", "localhost:1115")
	if err != nil {
		log.Fatal(err)
	}

	err = client.Call("Router.AddFrontend", &strowger.Config{Service: "example-server", HTTPDomain: os.Args[1]}, &struct{}{})
	if err != nil {
		log.Fatal(err)
	}
}
