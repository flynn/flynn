package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-docopt"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/pkg/cluster"
)

func init() {
	Register("log", runLog, "usage: flynn-host log [-f|--follow] ID")
}

func runLog(args *docopt.Args, client cluster.Host) error {
	return getLog(args.String["ID"], client, args.Bool["-f"] || args.Bool["--follow"], os.Stdout, os.Stderr)
}

func getLog(id string, client cluster.Host, follow bool, stdout, stderr io.Writer) error {
	attachReq := &host.AttachReq{
		JobID: id,
		Flags: host.AttachFlagStdout | host.AttachFlagStderr | host.AttachFlagLogs,
	}
	if follow {
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
	attachClient.Receive(stdout, stderr)
	return nil
}
