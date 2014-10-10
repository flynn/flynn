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
	Register("inspect", runInspect, `
usage: flynn-host inspect ID

Get low-level information about a job.`)
}

func runInspect(args *docopt.Args, client *cluster.Client) error {
	hostID, jobID, err := cluster.ParseJobID(args.String["ID"])
	if err != nil {
		return err
	}
	hostClient, err := client.DialHost(hostID)
	if err != nil {
		return fmt.Errorf("could not connect to host %s: %s", hostID, err)
	}
	job, err := hostClient.GetJob(jobID)
	if err != nil {
		return fmt.Errorf("no such job")
	}

	printJobDesc(job, os.Stdout)
	return nil
}

func printJobDesc(job *host.ActiveJob, out io.Writer) {
	w := tabwriter.NewWriter(out, 1, 2, 2, ' ', 0)
	defer w.Flush()
	fmt.Fprintln(w, "ID\t", clusterJobID(*job))
	fmt.Fprintln(w, "Status\t", job.Status)
	fmt.Fprintln(w, "StartedAt\t", job.StartedAt)
	fmt.Fprintln(w, "EndedAt\t", job.EndedAt)
	fmt.Fprintln(w, "ExitStatus\t", job.ExitStatus)
	fmt.Fprintln(w, "IP Address\t", job.InternalIP)
	for k, v := range job.Job.Metadata {
		fmt.Fprintln(w, k, "\t", v)
	}
}
