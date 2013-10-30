package main

import (
	"log"

	"github.com/flynn/rpcplus"
	"github.com/flynn/strowger/types"
)

func main() {
	client, err := rpcplus.DialHTTP("tcp", "localhost:1115")
	if err != nil {
		log.Fatal(err)
	}

	err = client.Call("Router.AddFrontend", &strowger.Config{Service: "example-server", HTTPDomain: "example.com"}, &struct{}{})
	if err != nil {
		log.Fatal(err)
	}
}
