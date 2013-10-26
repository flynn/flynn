package main

import (
	"github.com/flynn/go-discover/discover"
	"log"
)

type clientCmd struct {
	client *discover.DiscoverClient
}

func (cmd *clientCmd) InitClient() {
	client, err := discover.NewClient()
	if err != nil {
		log.Fatal("Error making client: ", err.Error())
	}
	cmd.client = client
}
