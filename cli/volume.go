package main

import (
	"fmt"
	"strings"

	"github.com/flynn/flynn/controller/client"
	"github.com/flynn/go-docopt"
)

func init() {
	register("volume", runVolume, `
usage: flynn volume
       flynn volume add <name> <url>

Manage resource volumes associated with the controller.

Commands:
    With no arguments, displays current volumes

    add  creates a new volume <name> at <url>
`)
}

func runVolume(args *docopt.Args, client controller.Client) error {
	volumes, err := client.VolumeList()
	if err != nil {
		return err
	}
	if len(volumes) == 0 {
		return nil
	}

	w := tabWriter()
	defer w.Flush()

	listRec(w, "ID", "HOST", "ATTACHMENTS (job_id - target - writeable)")
	for _, v := range volumes {
		attachments := make([]string, 0, len(v.Attachments))
		for _, a := range v.Attachments {
			attachments = append(attachments, fmt.Sprintf("%s - %s - %t", a.JobID, a.Target, a.Writeable))
		}
		listRec(w, v.ID, v.HostID, strings.Join(attachments, ","))
	}

	return nil
}
