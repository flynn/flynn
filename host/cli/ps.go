package cli

import (
	"fmt"
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
	Register("ps", runPs, "usage: flynn-host ps [-a|--all] [-q|--quiet]")
}

type sortJobs []host.ActiveJob

func (s sortJobs) Len() int           { return len(s) }
func (s sortJobs) Less(i, j int) bool { return s[i].StartedAt.Sub(s[j].StartedAt) < 0 }
func (s sortJobs) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }

func runPs(args *docopt.Args, client cluster.Host) error {
	all, err := client.ListJobs()
	if err != nil {
		return fmt.Errorf("could not get local jobs: %s", err)
	}

	jobs := make(sortJobs, 0, len(all))
	for _, job := range all {
		if !args.Bool["-a"] && !args.Bool["--all"] && job.Status != host.StatusStarting && job.Status != host.StatusRunning {
			continue
		}
		jobs = append(jobs, job)
	}
	sort.Sort(sort.Reverse(jobs))

	if args.Bool["-q"] || args.Bool["--quiet"] {
		for _, job := range jobs {
			fmt.Println(job.Job.ID)
		}
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 1, 2, 2, ' ', 0)
	defer w.Flush()
	fmt.Fprintln(w, "JOB ID\tSTATE\tSTARTED\tCONTROLLER APP\tCONTROLLER TYPE")
	for _, job := range jobs {
		fmt.Fprintf(w, "%s\t%s\t%s ago\t%s\t%s\n", job.Job.ID, job.Status, units.HumanDuration(time.Now().UTC().Sub(job.StartedAt)), job.Job.Metadata["flynn-controller.app_name"], job.Job.Metadata["flynn-controller.type"])
	}
	return nil
}
