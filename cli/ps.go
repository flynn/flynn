package main

import (
	"fmt"
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
usage: flynn ps [-a] [-c] [-q]

List flynn jobs.

Options:
  -a, --all      Show all jobs (default is running and pending)
  -c, --command  Show command
	-q, --quiet    Only display IDs

Example:

       $ flynn ps
       ID                                          TYPE  STATE  CREATED             RELEASE
       host0-52aedfbf-e613-40f2-941a-d832d10fc400  web   up     About a minute ago  cf39a906-38d1-4393-a6b1-8ad2befe8142
       host0-205595d8-206a-46a2-be30-2e98f53df272  web   up     25 seconds ago      cf39a906-38d1-4393-a6b1-8ad2befe8142
       host0-0f34548b-72fa-41fe-a425-abc4ac6a3857  web   up     25 seconds ago      cf39a906-38d1-4393-a6b1-8ad2befe8142

       $ flynn ps --all --command
       ID                                          TYPE  STATE  CREATED             RELEASE				  COMMAND
       host0-52aedfbf-e613-40f2-941a-d832d10fc400  web   up     2 minutes ago       cf39a906-38d1-4393-a6b1-8ad2befe842	  /runner/init start web
       host0-205595d8-206a-46a2-be30-2e98f53df272  web   up     About a minute ago  cf39a906-38d1-4393-a6b1-8ad2befe842	  /runner/init start web
       host0-0f34548b-72fa-41fe-a425-abc4ac6a3857  web   up     About a minute ago  cf39a906-38d1-4393-a6b1-8ad2befe842	  /runner/init start web
       host0-129b821f-3195-4b3b-b04b-669196cfbb03  run   down   5 seconds ago       cf39a906-38d1-4393-a6b1-8ad2befe842	  /runner/init /bin/bash

			 $ flynn ps --all --quiet
       host0-52aedfbf-e613-40f2-941a-d832d10fc400
       host0-205595d8-206a-46a2-be30-2e98f53df272
       host0-0f34548b-72fa-41fe-a425-abc4ac6a3857
       host0-129b821f-3195-4b3b-b04b-669196cfbb03
`)
}

func runPs(args *docopt.Args, client controller.Client) error {
	jobs, err := client.JobList(mustApp())
	if err != nil {
		return err
	}
	all := args.Bool["--all"] || args.Bool["-a"]
	if args.Bool["--quiet"] || args.Bool["-q"] {
		printJobsQuiet(jobs, all)
	} else {
		printJobs(jobs, all, args.Bool["--command"] || args.Bool["-c"])
	}
	return nil
}

func printJobsQuiet(jobs []*ct.Job, all bool) {
	sort.Sort(sortJobs(jobs))
	for _, j := range jobs {
		if !all && j.State != ct.JobStateUp && j.State != ct.JobStatePending {
			continue
		}
		fmt.Println(j.ID)
	}
}

func printJobs(jobs []*ct.Job, all bool, commandFlagOn bool) {
	sort.Sort(sortJobs(jobs))
	w := tabWriter()
	defer w.Flush()

	headers := []interface{}{"ID", "TYPE", "STATE", "CREATED", "RELEASE"}
	if commandFlagOn {
		headers = append(headers, "COMMAND")
	}
	listRec(w, headers...)
	for _, j := range jobs {
		if !all && j.State != ct.JobStateUp && j.State != ct.JobStatePending {
			continue
		}
		if j.Type == "" {
			j.Type = "run"
		}
		id := j.ID
		if id == "" {
			id = j.UUID
		}
		var created string
		if j.CreatedAt != nil {
			created = units.HumanDuration(time.Now().UTC().Sub(*j.CreatedAt)) + " ago"
		}
		fields := []interface{}{id, j.Type, j.State, created, j.ReleaseID}
		if commandFlagOn {
			fields = append(fields, strings.Join(j.Args, " "))
		}
		listRec(w, fields...)
	}
}

// sortJobs sorts Jobs in chronological order based on their CreatedAt time
type sortJobs []*ct.Job

func (s sortJobs) Len() int { return len(s) }
func (s sortJobs) Less(i, j int) bool {
	return s[i].CreatedAt == nil || s[j].CreatedAt != nil && (*s[j].CreatedAt).Sub(*s[i].CreatedAt) > 0
}
func (s sortJobs) Swap(i, j int) { s[i], s[j] = s[j], s[i] }
