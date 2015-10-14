package cli

import (
	"errors"
	"os"
	"text/tabwriter"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-docopt"
	"github.com/flynn/flynn/pkg/cluster"
)

func init() {
	Register("list", runListHosts, `
usage: flynn-host list

Example:

  $ flynn-host list
  ID    ADDR
  host  10.0.2.15:1113

Lists ID and IP of each host`)
}

func runListHosts(args *docopt.Args) error {
	clusterClient := cluster.NewClient()
	hosts, err := clusterClient.Hosts()
	if err != nil {
		return err
	}
	if len(hosts) == 0 {
		return errors.New("no hosts found")
	}

	w := tabwriter.NewWriter(os.Stdout, 1, 2, 2, ' ', 0)
	defer w.Flush()
	listRec(w, "ID", "ADDR")
	for _, h := range hosts {
		listRec(w, h.ID(), h.Addr())
	}
	return nil
}
