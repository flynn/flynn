package main

import (
	"io"
	"os"

	"github.com/flynn/flynn-controller/client"
	"github.com/flynn/go-flynn/demultiplex"
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
	var stdout io.Reader
	var done chan struct{}
	if logSplitOut {
		var stderr io.Reader
		stdout, stderr = demultiplex.Streams(rc)
		done = make(chan struct{})
		go func() {
			io.Copy(os.Stderr, stderr)
			close(done)
		}()
	} else {
		stdout = demultiplex.Clean(rc)
	}
	io.Copy(os.Stdout, stdout)
	if done != nil {
		<-done
	}
	rc.Close()
	return nil
}
