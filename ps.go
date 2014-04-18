package main

import (
	"fmt"
	"os"
	"sort"
	"text/tabwriter"

	"github.com/flynn/flynn-controller/client"
	ct "github.com/flynn/flynn-controller/types"
)

var cmdPs = &Command{
	Run:   runPs,
	Usage: "ps",
	Short: "list jobs",
	Long:  `Lists jobs.`,
}

func runPs(cmd *Command, args []string, client *controller.Client) error {
	jobs, err := client.GetJobList(mustApp())
	if err != nil {
		return err
	}

	if len(jobs) == 0 {
		return nil
	}

	sort.Sort(jobsByType(jobs))
	w := tabwriter.NewWriter(os.Stdout, 0, 8, 0, '\t', 0)

	fmt.Fprintln(w, "ID\tTYPE")
	for _, j := range jobs {
		if j.Type == "" {
			j.Type = "run"
		}
		fmt.Fprintf(w, "%s\t%s\n", j.ID, j.Type)
	}
	w.Flush()

	return nil
}

type jobsByType []*ct.Job

func (p jobsByType) Len() int           { return len(p) }
func (p jobsByType) Less(i, j int) bool { return p[i].Type < p[i].Type }
func (p jobsByType) Swap(i, j int)      { p[i], p[j] = p[j], p[i] }
