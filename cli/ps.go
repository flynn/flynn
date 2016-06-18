package main

import (
	"sort"
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
	ID                                         TYPE  STATE    CREATED         RELEASE
	host-f25797dc-c956-4337-89af-d49eff50f58e  web   up       14 seconds ago  1b1db8ef-ba4d-4314-85c1-d5895a44b27e
	6ec25d6e-2985-4807-8e64-02dc23c348bc       web   pending  7 seconds ago   1b1db8ef-ba4d-4314-85c1-d5895a44b27e
	ab14754c-73b7-4212-a6d9-73b825587fd2       web   pending  2 seconds ago   1b1db8ef-ba4d-4314-85c1-d5895a44b27e

	$ flynn ps --all
	ID                                         TYPE  STATE    CREATED             RELEASE
	host-d84dc657-83b8-4a62-aab8-ad97bb994761  web   down     2 minutes ago       cd698657-2955-4fa4-bc2f-8714b218a7a2
	host-feaff633-a37b-4565-9ade-24bae0cfae03  web   down     About a minute ago  cd698657-2955-4fa4-bc2f-8714b218a7a2
	host-e8b4f9be-e422-481f-928c-4c82f0bb5e8b  web   down     About a minute ago  cd698657-2955-4fa4-bc2f-8714b218a7a2
	host-95747b4f-fdcd-4c44-ab72-b3d9609668e4  run   down     54 seconds ago      cd698657-2955-4fa4-bc2f-8714b218a7a2
	host-cef95d8c-a632-4ae6-8a57-e13cbc24afa9  web   down     25 seconds ago      1b1db8ef-ba4d-4314-85c1-d5895a44b27e
	host-ef6249e3-b463-4fea-a7dd-b1302872f821  web   down     20 seconds ago      1b1db8ef-ba4d-4314-85c1-d5895a44b27e
	host-f25797dc-c956-4337-89af-d49eff50f58e  web   up       14 seconds ago      1b1db8ef-ba4d-4314-85c1-d5895a44b27e
	6ec25d6e-2985-4807-8e64-02dc23c348bc       web   pending  7 seconds ago       1b1db8ef-ba4d-4314-85c1-d5895a44b27e
	ab14754c-73b7-4212-a6d9-73b825587fd2       web   pending  2 seconds ago       1b1db8ef-ba4d-4314-85c1-d5895a44b27e
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

	listRec(w, "ID", "TYPE", "STATE", "CREATED", "RELEASE")
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
		listRec(w, id, j.Type, j.State, created, j.ReleaseID)
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
