package main

import (
	"io"
	"os"

	"github.com/flynn/flynn-controller/client"
)

var cmdLog = &Command{
	Run:   runLog,
	Usage: "log <job>",
	Short: "get job log",
	Long:  `Stream log for a specific job`,
}

func runLog(cmd *Command, args []string, client *controller.Client) error {
	if len(args) != 1 {
		cmd.printUsage(true)
	}

	rc, err := client.GetJobLog(mustApp(), args[0])
	if err != nil {
		return err
	}
	io.Copy(os.Stdout, rc)
	rc.Close()
	return nil
}
