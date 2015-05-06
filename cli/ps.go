package main

import (
	"sort"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-docopt"
	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
)

func init() {
	register("ps", runPs, `
usage: flynn ps

List flynn jobs.

Example:

	$ flynn ps
	ID                                      TYPE  RELEASE
	flynn-bb97c7dac2fa455dad73459056fabac2  web   b69d7fb5308a4684a09b160b82d267ec
	flynn-c59e02b3e6ad49809424848809d4749a  web   b69d7fb5308a4684a09b160b82d267ec
	flynn-46f0d715a9684e4c822e248e84a5a418  web   b69d7fb5308a4684a09b160b82d267ec
`)
}

func runPs(args *docopt.Args, client *controller.Client) error {
	jobs, err := client.JobList(mustApp())
	if err != nil {
		return err
	}
	sort.Sort(jobsByType(jobs))

	w := tabWriter()
	defer w.Flush()

	listRec(w, "ID", "TYPE", "RELEASE")
	for _, j := range jobs {
		if j.Type == "" {
			j.Type = "run"
		}
		if j.State != "up" {
			continue
		}
		listRec(w, j.ID, j.Type, j.ReleaseID)
	}

	return nil
}

type jobsByType []*ct.Job

func (p jobsByType) Len() int           { return len(p) }
func (p jobsByType) Less(i, j int) bool { return p[i].Type < p[j].Type }
func (p jobsByType) Swap(i, j int)      { p[i], p[j] = p[j], p[i] }
