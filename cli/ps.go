package main

import (
	"sort"
	"strings"
	"time"

	"github.com/docker/go-units"
	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/go-docopt"
)

func init() {
	register("ps", runPs, `
usage: flynn ps [-a]

List flynn jobs.

Options:
  -a, --all      Show all jobs (default is running and pending)

Example:

       $ flynn ps
       ID                                          TYPE  STATE  CREATED             RELEASE                               COMMAND
       host0-52aedfbf-e613-40f2-941a-d832d10fc400  web   up     About a minute ago  cf39a906-38d1-4393-a6b1-8ad2befe8142  /runner/init start web
       host0-205595d8-206a-46a2-be30-2e98f53df272  web   up     25 seconds ago      cf39a906-38d1-4393-a6b1-8ad2befe8142  /runner/init start web
       host0-0f34548b-72fa-41fe-a425-abc4ac6a3857  web   up     25 seconds ago      cf39a906-38d1-4393-a6b1-8ad2befe8142  /runner/init start web

       $ flynn ps --all
       ID                                          TYPE  STATE  CREATED             RELEASE				  COMMAND
       host0-52aedfbf-e613-40f2-941a-d832d10fc400  web   up     2 minutes ago       cf39a906-38d1-4393-a6b1-8ad2befe842	  /runner/init start web
       host0-205595d8-206a-46a2-be30-2e98f53df272  web   up     About a minute ago  cf39a906-38d1-4393-a6b1-8ad2befe842	  /runner/init start web
       host0-0f34548b-72fa-41fe-a425-abc4ac6a3857  web   up     About a minute ago  cf39a906-38d1-4393-a6b1-8ad2befe842	  /runner/init start web
       host0-129b821f-3195-4b3b-b04b-669196cfbb03  run   down   5 seconds ago       cf39a906-38d1-4393-a6b1-8ad2befe842	  /runner/init /bin/bash
`)
}

func runPs(args *docopt.Args, client controller.Client) error {
	jobs, err := client.JobList(mustApp())
	if err != nil {
		return err
	}
	sort.Sort(sortJobs(jobs))

	w := tabWriter()
	defer w.Flush()

	listRec(w, "ID", "TYPE", "STATE", "CREATED", "RELEASE", "COMMAND")
	for _, j := range jobs {
		if j.Type == "" {
			j.Type = "run"
		}
		if !args.Bool["--all"] && j.State != ct.JobStateUp && j.State != ct.JobStatePending {
			continue
		}
		id := j.ID
		if id == "" {
			id = j.UUID
		}
		var created string
		if j.CreatedAt != nil {
			created = units.HumanDuration(time.Now().UTC().Sub(*j.CreatedAt)) + " ago"
		}
		cmd := strings.Join(j.Args, " ")
		listRec(w, id, j.Type, j.State, created, j.ReleaseID, cmd)
	}

	return nil
}

// sortJobs sorts Jobs in chronological order based on their CreatedAt time
type sortJobs []*ct.Job

func (s sortJobs) Len() int { return len(s) }
func (s sortJobs) Less(i, j int) bool {
	return s[i].CreatedAt == nil || s[j].CreatedAt != nil && (*s[j].CreatedAt).Sub(*s[i].CreatedAt) > 0
}
func (s sortJobs) Swap(i, j int) { s[i], s[j] = s[j], s[i] }
