package main

import (
	"fmt"
	"github.com/flynn/rpcplus"
	"github.com/progrium/go-discover/discover"
	"log"
)

func main() {
	client, err := rpcplus.DialHTTP("tcp", "127.0.0.1:1111")
	if err != nil {
		log.Fatal("dialing:", err)
	}
	fmt.Printf("Connected\n")
	args := &discover.Args{
		Name: "rpc-test",
		Addr: "127.0.0.1",
	}
	var reply bool
	err = client.Call("DiscoverAgent.Register", args, &reply)
	if err != nil {
		log.Fatal("register error:", err)
	}
	fmt.Printf("register success\n")

	updates := make(chan *discover.ServiceUpdate, 10)
	client.StreamGo("DiscoverAgent.Subscribe", &discover.Args{
		Name: "test_subscribe",
	}, updates)
	for update := range updates {
		fmt.Printf("received: %b\n", update)
	}
}
