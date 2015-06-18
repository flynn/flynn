package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"text/tabwriter"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/docker/docker/pkg/units"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-docopt"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/pkg/cluster"
)

func init() {
	Register("ps", runPs, `
usage: flynn-host ps [-a|--all] [-q|--quiet]

List jobs`)
}

type sortJobs []host.ActiveJob

func (s sortJobs) Len() int           { return len(s) }
func (s sortJobs) Less(i, j int) bool { return s[i].StartedAt.Sub(s[j].StartedAt) < 0 }
func (s sortJobs) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }

func runPs(args *docopt.Args, client *cluster.Client) error {
	jobs, err := jobList(client, args.Bool["-a"] || args.Bool["--all"])
	if err != nil {
		return err
	}
	if args.Bool["-q"] || args.Bool["--quiet"] {
		for _, job := range jobs {
			fmt.Println(clusterJobID(job))
		}
		return nil
	}
	printJobs(jobs, os.Stdout)
	return nil
}

func jobList(client *cluster.Client, all bool) (sortJobs, error) {
	hosts, err := client.Hosts()
	if err != nil {
		return nil, fmt.Errorf("could not list hosts: %s", err)
	}
	if len(hosts) == 0 {
		return nil, errors.New("no hosts found")
	}

	var jobs []host.ActiveJob
	for _, h := range hosts {
		hostJobs, err := h.ListJobs()
		if err != nil {
			return nil, fmt.Errorf("could not get jobs for host %s: %s", h.ID(), err)
		}
		for _, job := range hostJobs {
			jobs = append(jobs, job)
		}
	}

	sorted := make(sortJobs, 0, len(jobs))
	for _, job := range jobs {
		if !all && job.Status != host.StatusStarting && job.Status != host.StatusRunning {
			continue
		}
		sorted = append(sorted, job)
	}
	sort.Sort(sort.Reverse(sorted))
	return sorted, nil
}

func printJobs(jobs sortJobs, out io.Writer) {
	w := tabwriter.NewWriter(out, 1, 2, 2, ' ', 0)
	defer w.Flush()
	listRec(w,
		"ID",
		"STATE",
		"STARTED",
		"CONTROLLER APP",
		"CONTROLLER TYPE",
	)

	for _, job := range jobs {
		var started string
		if !job.StartedAt.IsZero() {
			started = units.HumanDuration(time.Now().UTC().Sub(job.StartedAt)) + " ago"
		}

		listRec(w,
			clusterJobID(job),
			job.Status,
			started,
			job.Job.Metadata["flynn-controller.app_name"],
			job.Job.Metadata["flynn-controller.type"],
		)
	}
}

func clusterJobID(job host.ActiveJob) string {
	return job.HostID + "-" + job.Job.ID
}
