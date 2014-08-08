package main

import (
	"io"
	"os"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-docopt"
	"github.com/flynn/flynn/controller/client"
	"github.com/flynn/flynn/pkg/cluster"
)

func runLog(argv []string, client *controller.Client) error {
	usage := `usage: flynn log [options] <job>

Stream log for a specific job.

Options:
    -s, --split-stderr    send stderr lines to stderr
	`
	args, _ := docopt.Parse(usage, argv, true, "", false)

	rc, err := client.GetJobLog(mustApp(), args.String["<job>"])
	if err != nil {
		return err
	}
	var stderr io.Writer = os.Stdout
	if args.Bool["--split-stderr"] {
		stderr = os.Stderr
	}
	attachClient := cluster.NewAttachClient(struct {
		io.Writer
		io.ReadCloser
	}{nil, rc})
	attachClient.Receive(os.Stdout, stderr)
	return nil
}
