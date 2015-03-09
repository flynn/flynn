package main

import (
	"fmt"
	"log"
	"os"

	"github.com/flynn/flynn/controller/client"
)

func main() {
	appName := os.Args[2]

	client, err := controller.NewClient("", os.Getenv("CONTROLLER_AUTH_KEY"))
	if err != nil {
		log.Fatalln("Unable to connect to controller:", err)
	}
	app, err := client.GetApp(appName)
	if err != nil {
		log.Fatalln("Error retrieving app:", err)
	}

	fmt.Println(app.ID)
}
