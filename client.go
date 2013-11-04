package main

import (
	"github.com/flynn/go-discover/discover"
	"log"
	"os"
)

type clientCmd struct {
	client *discover.Client
}

func (cmd *clientCmd) InitClient(silent bool) {
	client, err := discover.NewClient()
	if err != nil {
		if silent {
			os.Exit(1)
		}
		log.Fatal("Error making client: ", err.Error())
	}
	cmd.client = client
}
