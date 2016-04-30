package main

import (
	"net/http"
	"os"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/Sirupsen/logrus"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/docker/distribution/configuration"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/docker/distribution/context"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/docker/distribution/registry/handlers"
	_ "github.com/flynn/flynn/Godeps/_workspace/src/github.com/docker/distribution/registry/storage/driver/filesystem"
	"github.com/flynn/flynn/pkg/version"
)

// main is a modified version of the registry main function:
// https://github.com/docker/distribution/blob/6ba799b/cmd/registry/main.go
func main() {
	logrus.SetLevel(logrus.InfoLevel)

	ctx := context.Background()
	ctx = context.WithValue(ctx, "version", version.String())
	ctx = context.WithLogger(ctx, context.GetLogger(ctx, "version"))

	config := configuration.Configuration{
		Version: configuration.CurrentVersion,
		Storage: configuration.Storage{
			"filesystem": configuration.Parameters{
				"rootdirectory": "/data",
			},
		},
	}

	app := handlers.NewApp(ctx, config)
	// TODO: add status handler

	addr := ":" + os.Getenv("PORT")
	context.GetLogger(app).Infof("listening on %s", addr)
	if err := http.ListenAndServe(addr, app); err != nil {
		context.GetLogger(app).Fatalln(err)
	}
}
