package cli

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/flynn/flynn/pkg/cluster"
	"github.com/flynn/go-docopt"
)

func init() {
	Register("log-sink", runLogSink, `
usage: flynn-host log-sink list <host>

Commands:
    list    Display a list of sinks configured for a host

Examples:

    $ flynn-host log-sink list host1
`)
}

func runLogSink(args *docopt.Args, client *cluster.Client) error {
	switch {
	case args.Bool["list"]:
		return runLogSinkList(args, client)
	}
	return nil
}

func runLogSinkList(args *docopt.Args, client *cluster.Client) error {
	hostClient, err := client.Host(args.String["<host>"])
	if err != nil {
		return fmt.Errorf("could not connect to host: %s", err)
	}
	sinks, err := hostClient.GetSinks()
	if err != nil {
		return err
	}

	w := tabwriter.NewWriter(os.Stdout, 1, 2, 2, ' ', 0)
	defer w.Flush()
	listRec(w,
		"ID",
		"KIND",
		"CONFIG",
		"HOST MANAGED",
	)

	for _, sink := range sinks {
		listRec(w,
			sink.ID,
			sink.Kind,
			string(sink.Config),
			sink.HostManaged,
		)
	}
	return nil
}
