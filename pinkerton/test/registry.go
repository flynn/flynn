package main

import (
	"log"
	"net/http"
	"os"

	"github.com/docker/distribution/configuration"
	"github.com/docker/distribution/context"
	"github.com/docker/distribution/registry/handlers"
	_ "github.com/docker/distribution/registry/storage/driver/filesystem"
)

func main() {
	root := os.Args[1]
	config := configuration.Configuration{
		Storage: configuration.Storage{
			"filesystem": configuration.Parameters{
				"rootdirectory": root,
			},
		},
	}
	app := handlers.NewApp(context.Background(), config)
	if err := http.ListenAndServe(":8080", app); err != nil {
		log.Fatal(err)
	}
}
