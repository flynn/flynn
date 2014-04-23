package main

import (
	"sort"

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
	jobs, err := client.JobList(mustApp())
	if err != nil {
		return err
	}
	if len(jobs) == 0 {
		return nil
	}
	sort.Sort(jobsByType(jobs))

	w := tabWriter()
	defer w.Flush()

	listRec(w, "ID", "TYPE")
	for _, j := range jobs {
		if j.Type == "" {
			j.Type = "run"
		}
		listRec(w, j.ID, j.Type)
	}

	return nil
}

type jobsByType []*ct.Job

func (p jobsByType) Len() int           { return len(p) }
func (p jobsByType) Less(i, j int) bool { return p[i].Type < p[i].Type }
func (p jobsByType) Swap(i, j int)      { p[i], p[j] = p[j], p[i] }
