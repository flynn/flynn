package cli

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-docopt"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/pkg/cluster"
)

func init() {
	Register("log", runLog, `
usage: flynn-host log [--init] [-f|--follow] ID

Get the logs of a job`)
}

func runLog(args *docopt.Args, client *cluster.Client) error {
	hostID, jobID, err := cluster.ParseJobID(args.String["ID"])
	if err != nil {
		return err
	}
	return getLog(
		hostID,
		jobID,
		client,
		args.Bool["-f"] || args.Bool["--follow"],
		args.Bool["--init"],
		os.Stdout,
		os.Stderr,
	)
}

func getLog(hostID, jobID string, client *cluster.Client, follow, init bool, stdout, stderr io.Writer) error {
	hostClient, err := client.Host(hostID)
	if err != nil {
		return fmt.Errorf("could not connect to host %s: %s", hostID, err)
	}
	attachReq := &host.AttachReq{
		JobID: jobID,
		Flags: host.AttachFlagStdout | host.AttachFlagStderr | host.AttachFlagLogs,
	}
	if follow {
		attachReq.Flags |= host.AttachFlagStream
	}
	if init {
		attachReq.Flags |= host.AttachFlagInitLog
	}
	attachClient, err := hostClient.Attach(attachReq, false)
	if err != nil {
		if err == cluster.ErrWouldWait {
			return errors.New("no such job")
		}
		return err
	}
	defer attachClient.Close()
	_, err = attachClient.Receive(stdout, stderr)
	return err
}
