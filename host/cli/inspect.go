package cli

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-docopt"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/pkg/cluster"
)

func init() {
	Register("inspect", runInspect, `
usage: flynn-host inspect [options] ID

Get low-level information about a job.

options:
  --omit-env  don't include the job environment, which may be sensitive
`)
}

func runInspect(args *docopt.Args, client *cluster.Client) error {
	jobID := args.String["ID"]
	hostID, err := cluster.ExtractHostID(jobID)
	if err != nil {
		return err
	}
	hostClient, err := client.Host(hostID)
	if err != nil {
		return fmt.Errorf("could not connect to host %s: %s", hostID, err)
	}
	job, err := hostClient.GetJob(jobID)
	if err != nil {
		return fmt.Errorf("no such job")
	}

	printJobDesc(job, os.Stdout, !args.Bool["--omit-env"])
	return nil
}

func displayTime(ts time.Time) string {
	if ts.IsZero() {
		return ""
	}
	return ts.String()
}

func printJobDesc(job *host.ActiveJob, out io.Writer, env bool) {
	w := tabwriter.NewWriter(out, 1, 2, 2, ' ', 0)
	defer w.Flush()

	var exitStatus string
	if job.ExitStatus != nil {
		exitStatus = strconv.Itoa(*job.ExitStatus)
	}
	var jobError string
	if job.Error != nil {
		jobError = *job.Error
	}

	listRec(w, "ID", job.Job.ID)
	listRec(w, "Entrypoint", strings.Join(job.Job.Config.Entrypoint, " "))
	listRec(w, "Cmd", strings.Join(job.Job.Config.Cmd, " "))
	listRec(w, "Status", job.Status)
	listRec(w, "CreatedAt", job.CreatedAt)
	listRec(w, "StartedAt", job.StartedAt)
	listRec(w, "EndedAt", displayTime(job.EndedAt))
	listRec(w, "ExitStatus", exitStatus)
	listRec(w, "Error", jobError)
	listRec(w, "IP Address", job.InternalIP)
	listRec(w, "ImageArtifact", job.Job.ImageArtifact.URI)
	for i, artifact := range job.Job.FileArtifacts {
		listRec(w, fmt.Sprintf("FileArtifact[%d]", i), artifact.URI)
	}
	for k, v := range job.Job.Metadata {
		listRec(w, k, v)
	}
	if env {
		for k, v := range job.Job.Config.Env {
			listRec(w, fmt.Sprintf("ENV[%s]", k), v)
		}
	}
}

func listRec(w io.Writer, a ...interface{}) {
	for i, x := range a {
		fmt.Fprint(w, x)
		if i+1 < len(a) {
			w.Write([]byte{'\t'})
		} else {
			w.Write([]byte{'\n'})
		}
	}
}
