package cli

import (
	"fmt"
	"io"
	"os"
	"text/tabwriter"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-docopt"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/pkg/cluster"
)

func init() {
	Register("inspect", runInspect, "usage: flynn-host inspect ID")
}

func runInspect(args *docopt.Args, client cluster.Host) error {
	job, err := client.GetJob(args.String["ID"])
	if err != nil {
		return fmt.Errorf("no such job")
	}

	printJobDesc(job, os.Stdout)
	return nil
}

func printJobDesc(job *host.ActiveJob, out io.Writer) {
	w := tabwriter.NewWriter(out, 1, 2, 2, ' ', 0)
	defer w.Flush()
	fmt.Fprintln(w, "ID\t", job.Job.ID)
	fmt.Fprintln(w, "Status\t", job.Status)
	fmt.Fprintln(w, "StartedAt\t", job.StartedAt)
	fmt.Fprintln(w, "EndedAt\t", job.EndedAt)
	fmt.Fprintln(w, "ExitStatus\t", job.ExitStatus)
	for k, v := range job.Job.Metadata {
		fmt.Fprintln(w, k, "\t", v)
	}
}
