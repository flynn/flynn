package main

import (
	"io"
	"os"

	"github.com/flynn/flynn/controller/client"
	"github.com/flynn/flynn/pkg/cluster"
)

var cmdLog = &Command{
	Run:   runLog,
	Usage: "log [-s] <job>",
	Short: "get job log",
	Long:  `Stream log for a specific job`,
}

var logSplitOut bool

func init() {
	cmdLog.Flag.BoolVarP(&logSplitOut, "split-stderr", "s", false, "send stderr lines to stderr")
}

func runLog(cmd *Command, args []string, client *controller.Client) error {
	if len(args) != 1 {
		cmd.printUsage(true)
	}

	rc, err := client.GetJobLog(mustApp(), args[0])
	if err != nil {
		return err
	}
	var stderr io.Writer = os.Stdout
	if logSplitOut {
		stderr = os.Stderr
	}
	attachClient := cluster.NewAttachClient(struct {
		io.Writer
		io.ReadCloser
	}{nil, rc})
	attachClient.Receive(os.Stdout, stderr)
	return nil
}
