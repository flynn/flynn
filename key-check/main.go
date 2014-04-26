package main

import (
	"log"
	"os"

	"github.com/flynn/flynn-controller/client"
)

func main() {
	key := os.Args[2]

	client, err := controller.NewClient("", os.Getenv("CONTROLLER_AUTH_KEY"))
	if err != nil {
		log.Fatalln("Unable to connect to controller:", err)
	}
	keys, err := client.KeyList()
	if err != nil {
		log.Fatalln("Error retrieving key list:", err)
	}

	for _, authKey := range keys {
		if key == authKey.Key {
			os.Exit(0)
		}
	}
	os.Exit(1)
}
