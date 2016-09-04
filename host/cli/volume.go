package cli

import (
	"errors"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/flynn/flynn/host/volume"
	"github.com/flynn/flynn/pkg/cluster"
	"github.com/flynn/go-docopt"
)

func init() {
	Register("volume", runVolume, `
usage: flynn-host volume list
       flynn-host volume create [--provider=<provider>] <host>
       flynn-host volume delete ID...
       flynn-host volume gc

Commands:
    list    Display a list of all volumes of known Flynn hosts
    create  Creates a volume on a host
    delete  Deletes volumes, destroying any data stored on them
    gc      Garbage collect currently unused volumes

Examples:

    $ flynn-host volume list

    $ flynn-host volume create --provider default host0

    $ flynn-host volume destroy 102fad07-07a3-4841-bded-d9e8a3eedbd6

    $ flynn-host volume gc
`)
}

func runVolume(args *docopt.Args, client *cluster.Client) error {
	switch {
	case args.Bool["list"]:
		return runVolumeList(args, client)
	case args.Bool["delete"]:
		return runVolumeDelete(args, client)
	case args.Bool["create"]:
		return runVolumeCreate(args, client)
	case args.Bool["gc"]:
		return runVolumeGarbageCollection(args, client)
	}
	return nil
}

func runVolumeGarbageCollection(args *docopt.Args, client *cluster.Client) error {
	// collect list of all volume ids currently attached to jobs
	hosts, err := client.Hosts()
	if err != nil {
		return fmt.Errorf("could not list hosts: %s", err)
	}
	if len(hosts) == 0 {
		return errors.New("no hosts found")
	}

	attached := make(map[string]struct{})
	for _, h := range hosts {
		jobs, err := h.ListJobs()
		if err != nil {
			fmt.Printf("error listing jobs on host %s: %s\n", h.ID(), err)
			continue
		}
		for _, j := range jobs {
			for _, vb := range j.Job.Config.Volumes {
				attached[vb.VolumeID] = struct{}{}
			}
		}
	}

	volumes, err := clusterVolumes(hosts)
	if err != nil {
		return err
	}

	// iterate over list of all volumes, deleting any not found in the attached list
	success := true
outer:
	for _, v := range volumes {
		if _, ok := attached[v.Volume.ID]; ok {
			// volume is attached, continue to next volume
			continue outer
		}
		if err := v.Host.DestroyVolume(v.Volume.ID); err != nil {
			success = false
			fmt.Printf("could not delete volume %s: %s\n", v.Volume.ID, err)
			continue outer
		}
		fmt.Println(v.Volume.ID, "deleted")
	}
	if !success {
		return errors.New("could not garbage collect all volumes")
	}

	return nil
}

func runVolumeDelete(args *docopt.Args, client *cluster.Client) error {
	success := true
	hosts, err := client.Hosts()
	if err != nil {
		return fmt.Errorf("could not list hosts: %s", err)
	}
	if len(hosts) == 0 {
		return errors.New("no hosts found")
	}

	volumes, err := clusterVolumes(hosts)
	if err != nil {
		return err
	}

outer:
	for _, id := range args.All["ID"].([]string) {
		// find this volume in the list
		for _, v := range volumes {
			if v.Volume.ID == id {
				if err := v.Host.DestroyVolume(id); err != nil {
					success = false
					fmt.Printf("could not delete volume %s: %s\n", id, err)
					continue outer
				}
				// delete the volume
				fmt.Println(id, "deleted")
				continue outer
			}
		}
		success = false
		fmt.Printf("could not delete volume %s: volume not found\n", id)
	}
	if !success {
		return errors.New("could not delete all volumes")
	}
	return nil
}

func runVolumeCreate(args *docopt.Args, client *cluster.Client) error {
	hostId := args.String["<host>"]
	hostClient, err := client.Host(hostId)
	if err != nil {
		fmt.Println("could not connect to host", hostId)
	}
	provider := "default"
	if args.String["--provider"] != "" {
		provider = args.String["--provider"]
	}
	v, err := hostClient.CreateVolume(provider)
	if err != nil {
		fmt.Printf("could not create volume: %s\n", err)
		return err
	}
	fmt.Printf("created volume %s on %s\n", v.ID, hostId)
	return nil
}

type hostVolume struct {
	Host   *cluster.Host
	Volume *volume.Info
}

func clusterVolumes(hosts []*cluster.Host) ([]hostVolume, error) {
	var volumes []hostVolume
	for _, h := range hosts {
		hostVolumes, err := h.ListVolumes()
		if err != nil {
			return volumes, fmt.Errorf("could not get volumes for host %s: %s", h.ID(), err)
		}
		for _, v := range hostVolumes {
			volumes = append(volumes, hostVolume{Host: h, Volume: v})
		}
	}
	return volumes, nil
}

func runVolumeList(args *docopt.Args, client *cluster.Client) error {
	hosts, err := client.Hosts()
	if err != nil {
		return fmt.Errorf("could not list hosts: %s", err)
	}
	if len(hosts) == 0 {
		return errors.New("no hosts found")
	}

	volumes, err := clusterVolumes(hosts)
	if err != nil {
		return err
	}

	w := tabwriter.NewWriter(os.Stdout, 1, 2, 2, ' ', 0)
	defer w.Flush()
	listRec(w,
		"ID",
		"HOST",
	)

	for _, volume := range volumes {
		listRec(w,
			volume.Volume.ID,
			volume.Host.ID(),
		)
	}
	return nil
}
