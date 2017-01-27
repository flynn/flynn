package cli

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/docker/go-units"
	"github.com/flynn/flynn/host/types"
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
    create  Creates a data volume on a host
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

	keep := make(map[string]struct{})
	for _, h := range hosts {
		jobs, err := h.ListJobs()
		if err != nil {
			fmt.Printf("error listing jobs on host %s: %s\n", h.ID(), err)
			continue
		}
		for _, j := range jobs {
			if j.Status != host.StatusRunning && j.Status != host.StatusStarting {
				continue
			}

			// keep the tmpfs (it has the same ID as the job)
			keep[j.Job.ID] = struct{}{}

			// keep the data volumes
			for _, vb := range j.Job.Config.Volumes {
				keep[vb.VolumeID] = struct{}{}
			}

			// keep the mounted layers
			for _, m := range j.Job.Mountspecs {
				keep[m.ID] = struct{}{}
			}
		}
	}

	volumes, err := clusterVolumes(hosts)
	if err != nil {
		return err
	}

	// iterate over list of all volumes, deleting any not found in the keep list
	success := true
outer:
	for _, v := range volumes {
		if _, ok := keep[v.Volume.ID]; ok {
			continue outer
		}
		// don't delete system images
		if v.Volume.Meta["flynn.system-image"] == "true" {
			continue
		}
		if err := v.Host.DestroyVolume(v.Volume.ID); err != nil {
			success = false
			fmt.Printf("could not delete %s volume %s: %s\n", v.Volume.Type, v.Volume.ID, err)
			continue outer
		}
		fmt.Println("Deleted", v.Volume.Type, "volume", v.Volume.ID)
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
	vol := &volume.Info{}
	if err := hostClient.CreateVolume(provider, vol); err != nil {
		fmt.Printf("could not create volume: %s\n", err)
		return err
	}
	fmt.Printf("created volume %s on %s\n", vol.ID, hostId)
	return nil
}

type hostVolume struct {
	Host   *cluster.Host
	Volume *volume.Info
}

type sortVolumes []hostVolume

func (s sortVolumes) Len() int           { return len(s) }
func (s sortVolumes) Less(i, j int) bool { return s[i].Volume.CreatedAt.Sub(s[j].Volume.CreatedAt) < 0 }
func (s sortVolumes) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }

func clusterVolumes(hosts []*cluster.Host) (sortVolumes, error) {
	var volumes sortVolumes
	for _, h := range hosts {
		hostVolumes, err := h.ListVolumes()
		if err != nil {
			return volumes, fmt.Errorf("could not get volumes for host %s: %s", h.ID(), err)
		}
		for _, v := range hostVolumes {
			volumes = append(volumes, hostVolume{Host: h, Volume: v})
		}
	}
	sort.Sort(volumes)
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
		"TYPE",
		"HOST",
		"CREATED",
		"META",
	)

	for _, volume := range volumes {
		meta := make([]string, 0, len(volume.Volume.Meta))
		for k, v := range volume.Volume.Meta {
			meta = append(meta, fmt.Sprintf("%s=%s", k, v))
		}
		listRec(w,
			volume.Volume.ID,
			volume.Volume.Type,
			volume.Host.ID(),
			units.HumanDuration(time.Now().UTC().Sub(volume.Volume.CreatedAt))+" ago",
			strings.Join(meta, " "),
		)
	}
	return nil
}
