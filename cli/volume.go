package main

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/docker/go-units"
	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/pkg/cluster"
	"github.com/flynn/go-docopt"
)

func init() {
	register("volume", runVolume, `
usage: flynn volume
       flynn volume show [--json] <id>
       flynn volume decommission <id>

Manage cluster volumes.

Commands:
    With no arguments, displays current volumes.

    show
	    Show information about a volume.

    decommission
	    Decommission a volume.

	    A decommissioned volume will continue to exist but will no longer
	    be attached to new jobs by the scheduler.
`)
}

func runVolume(args *docopt.Args, client controller.Client) error {
	if args.Bool["show"] {
		return runVolumeShow(args, client)
	} else if args.Bool["decommission"] {
		return runVolumeDecommission(args, client)
	}
	return runVolumeList(args, client)
}

func runVolumeList(args *docopt.Args, client controller.Client) error {
	volumes, err := client.AppVolumeList(mustApp())
	if err != nil {
		return err
	}

	w := tabWriter()
	defer w.Flush()

	listRec(w, "ID", "HOST", "STATE", "ATTACHED JOB", "CREATED", "DECOMMISSIONED")
	for _, v := range volumes {
		var jobID string
		if v.JobID != nil {
			jobID = cluster.GenerateJobID(v.HostID, *v.JobID)
		}
		var created string
		if v.CreatedAt != nil {
			created = units.HumanDuration(time.Now().UTC().Sub(*v.CreatedAt)) + " ago"
		}
		listRec(w, v.ID, v.HostID, v.State, jobID, created, v.DecommissionedAt != nil)
	}

	return nil
}

func runVolumeShow(args *docopt.Args, client controller.Client) error {
	vol, err := client.GetVolume(mustApp(), args.String["<id>"])
	if err != nil {
		return err
	}
	if args.Bool["--json"] {
		return json.NewEncoder(os.Stdout).Encode(vol)
	}
	w := tabwriter.NewWriter(os.Stdout, 1, 2, 2, ' ', 0)
	defer w.Flush()
	listRec(w, "ID:", vol.ID)
	listRec(w, "State:", vol.State)
	listRec(w, "HostID:", vol.HostID)
	listRec(w, "AppID:", vol.AppID)
	listRec(w, "ReleaseID:", vol.ReleaseID)
	var jobID string
	if vol.JobID != nil {
		jobID = cluster.GenerateJobID(vol.HostID, *vol.JobID)
	}
	listRec(w, "JobID:", jobID)
	listRec(w, "JobType:", vol.JobType)
	listRec(w, "CreatedAt:", vol.CreatedAt)
	listRec(w, "UpdatedAt:", vol.UpdatedAt)
	listRec(w, "DecommissionedAt:", vol.DecommissionedAt)
	return nil
}

func runVolumeDecommission(args *docopt.Args, client controller.Client) error {
	vol := &ct.Volume{ID: args.String["<id>"]}
	if err := client.DecommissionVolume(mustApp(), vol); err != nil {
		return err
	}
	fmt.Printf("volume %s successfully decommissioned at %s\n", vol.ID, vol.DecommissionedAt)
	return nil
}
