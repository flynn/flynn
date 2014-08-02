package main

import (
	"log"
	"os"

	"github.com/flynn/flynn/discoverd/client"
)

type clientCmd struct {
	client *discoverd.Client
}

func (cmd *clientCmd) InitClient(silent bool) {
	client, err := discoverd.NewClient()
	if err != nil {
		if silent {
			os.Exit(1)
		}
		log.Fatal("Error making client: ", err.Error())
	}
	cmd.client = client
}
