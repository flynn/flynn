package main

import (
	"sort"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-docopt"
	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
)

func init() {
	register("ps", runPs, `
usage: flynn ps [-a]

List flynn jobs.

Options:
  -a, --all      Show all jobs (default is running and pending)

Example:

	$ flynn ps
	ID                                         TYPE  STATE    RELEASE
	318810fb-4679-419b-aed4-b0838c71c0eb       web   pending  d2ab4264-a647-4dc2-ac8d-d5821a475962
	5bc89fe1-d4b5-4021-a337-10dd2b391358       web   pending  d2ab4264-a647-4dc2-ac8d-d5821a475962
	host-93612073-f06e-41d9-bdae-df3e45f8a11d  web   up       d2ab4264-a647-4dc2-ac8d-d5821a475962
	host-53dba8f4-561b-460b-b75b-677e8b6660fb  web   up       d2ab4264-a647-4dc2-ac8d-d5821a475962

	$ flynn ps --all
	ID                                         TYPE  STATE    RELEASE
	318810fb-4679-419b-aed4-b0838c71c0eb       web   pending  d2ab4264-a647-4dc2-ac8d-d5821a475962
	5bc89fe1-d4b5-4021-a337-10dd2b391358       web   pending  d2ab4264-a647-4dc2-ac8d-d5821a475962
	host-93612073-f06e-41d9-bdae-df3e45f8a11d  web   up       d2ab4264-a647-4dc2-ac8d-d5821a475962
	host-53dba8f4-561b-460b-b75b-677e8b6660fb  web   up       d2ab4264-a647-4dc2-ac8d-d5821a475962
	host-0f0472ad-272d-4bf3-a873-bc542cf16e31  web   down     d2ab4264-a647-4dc2-ac8d-d5821a475962
	host-12657e64-adb9-4121-904f-4896231f3bc4  web   down     d2ab4264-a647-4dc2-ac8d-d5821a475962
	host-c7f5d522-974d-4be1-8103-49f7eb309cb5  web   down     1a675dd8-9c44-468b-bef0-b3f8a25f5bdc
	host-380c5ea6-d503-454e-a2a4-6b20dc9f4b81  web   down     1a675dd8-9c44-468b-bef0-b3f8a25f5bdc
	host-cb4fed31-d84d-45d9-a566-c18907101f6f  web   down     1a675dd8-9c44-468b-bef0-b3f8a25f5bdc
	host-9af9b2a9-1c53-4616-985a-e81da943fb95  web   down     1a675dd8-9c44-468b-bef0-b3f8a25f5bdc
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

	listRec(w, "ID", "TYPE", "STATE", "RELEASE")
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
		listRec(w, id, j.Type, j.State, j.ReleaseID)
	}

	return nil
}

type jobsByType []*ct.Job

func (p jobsByType) Len() int           { return len(p) }
func (p jobsByType) Less(i, j int) bool { return p[i].Type < p[j].Type }
func (p jobsByType) Swap(i, j int)      { p[i], p[j] = p[j], p[i] }
