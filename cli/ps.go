package main

import (
	"sort"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-docopt"
	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
)

func runPs(argv []string, client *controller.Client) error {
	usage := `usage: flynn ps

List flynn jobs.
	`
	docopt.Parse(usage, argv, true, "", false)

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
		if j.State != "up" {
			continue
		}
		listRec(w, j.ID, j.Type)
	}

	return nil
}

type jobsByType []*ct.Job

func (p jobsByType) Len() int           { return len(p) }
func (p jobsByType) Less(i, j int) bool { return p[i].Type < p[i].Type }
func (p jobsByType) Swap(i, j int)      { p[i], p[j] = p[j], p[i] }
