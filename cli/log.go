package main

import (
	"io"
	"os"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-docopt"
	"github.com/flynn/flynn/controller/client"
	"github.com/flynn/flynn/pkg/cluster"
)

func init() {
	register("log", runLog, `
usage: flynn log [options] <job>

Stream log for a specific job.

Options:
	-s, --split-stderr  send stderr lines to stderr
	-f, --follow        stream new lines after printing log buffer
`)
}

func runLog(args *docopt.Args, client *controller.Client) error {
	rc, err := client.GetJobLog(mustApp(), args.String["<job>"], args.Bool["--follow"])
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
