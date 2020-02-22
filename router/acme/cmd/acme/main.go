package main

import (
	"context"

	"github.com/flynn/flynn/pkg/shutdown"
	"github.com/flynn/flynn/router/acme"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	shutdown.BeforeExit(cancel)
	if err := acme.RunService(ctx); err != nil {
		shutdown.Fatal(err)
	}
}
