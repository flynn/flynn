package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io/ioutil"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-docopt"
	tuf "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-tuf/client"
	"github.com/flynn/flynn/pinkerton/layer"
	"github.com/flynn/flynn/pkg/cluster"
)

func init() {
	Register("update", runUpdate, `
usage: flynn-host update [--driver=<name>] [--root=<path>] [--repository=<uri>] [--tuf-db=<path>]

Options:
  -d --driver=<name>       image storage driver [default: aufs]
  -r --root=<path>         image storage root [default: /var/lib/docker]
  -u --repository=<uri>    image repository URI [default: https://dl.flynn.io/images]
  -t --tuf-db=<path>       local TUF file [default: /etc/flynn/tuf.db]

Update Flynn components`)
}

func runUpdate(args *docopt.Args) error {
	// create and update a TUF client
	local, err := tuf.FileLocalStore(args.String["--tuf-db"])
	if err != nil {
		return err
	}
	remote, err := tuf.HTTPRemoteStore(args.String["--repository"], nil)
	if err != nil {
		return err
	}
	client := tuf.NewClient(local, remote)
	if _, err := client.Update(); err != nil && !tuf.IsLatestSnapshot(err) {
		return err
	}

	// read the TUF db so we can pass it to hosts
	tufDB, err := ioutil.ReadFile(args.String["--tuf-db"])
	if err != nil {
		return err
	}

	// get list of hosts
	clusterClient, err := cluster.NewClient()
	if err != nil {
		return err
	}
	hosts, err := clusterClient.ListHosts()
	if err != nil {
		return err
	}
	if len(hosts) == 0 {
		return errors.New("no hosts found")
	}

	hostErrs := make(chan error)
	for _, h := range hosts {
		go func(hostID string) {
			host, err := clusterClient.DialHost(hostID)
			if err != nil {
				hostErrs <- err
				return
			}
			ch := make(chan *layer.PullInfo)
			stream, err := host.PullImages(
				args.String["--repository"],
				args.String["--driver"],
				args.String["--root"],
				bytes.NewReader(tufDB),
				ch,
			)
			if err != nil {
				hostErrs <- err
				return
			}
			defer stream.Close()
			for info := range ch {
				fmt.Printf("==> %s : %s %s %s\n", hostID, info.Repo, info.ID, info.Status)
			}
			hostErrs <- stream.Err()
		}(h.ID)
	}
	var hostErr error
	for _, h := range hosts {
		if err := <-hostErrs; err != nil {
			fmt.Printf("update: error running update on host %s: %s\n", h.ID, err)
			hostErr = err
		}
	}
	return hostErr
}
