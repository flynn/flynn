package cli

import (
	"fmt"
	"os"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-docopt"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/pkg/cluster"
)

func init() {
	Register("log", runLog, "usage: flynn-host log [-f|--follow] ID")
}

func runLog(args *docopt.Args, client cluster.Host) error {
	attachReq := &host.AttachReq{
		JobID: args.String["ID"],
		Flags: host.AttachFlagStdout | host.AttachFlagStderr | host.AttachFlagLogs,
	}
	if args.Bool["-f"] || args.Bool["--follow"] {
		attachReq.Flags |= host.AttachFlagStream
	}
	attachClient, err := client.Attach(attachReq, false)
	if err != nil {
		if err == cluster.ErrWouldWait {
			return fmt.Errorf("no such job")
		}
		return err
	}
	defer attachClient.Close()
	attachClient.Receive(os.Stdout, os.Stderr)
	return nil
}
