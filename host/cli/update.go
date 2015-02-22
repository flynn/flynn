package cli

import (
	"bytes"
	"errors"
	"io/ioutil"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-docopt"
	tuf "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-tuf/client"
	"github.com/flynn/flynn/Godeps/_workspace/src/gopkg.in/inconshreveable/log15.v2"
	"github.com/flynn/flynn/pinkerton/layer"
	"github.com/flynn/flynn/pkg/cluster"
)

func init() {
	Register("update", runUpdate, `
usage: flynn-host update [--driver=<name>] [--root=<path>] [--repository=<uri>] [--tuf-db=<path>]

Options:
  -d --driver=<name>       image storage driver [default: aufs]
  -r --root=<path>         image storage root [default: /var/lib/docker]
  -u --repository=<uri>    image repository URI [default: https://dl.flynn.io/tuf]
  -t --tuf-db=<path>       local TUF file [default: /etc/flynn/tuf.db]

Update Flynn components`)
}

func runUpdate(args *docopt.Args) error {
	log := log15.New()

	// create and update a TUF client
	log.Info("initializing TUF client")
	local, err := tuf.FileLocalStore(args.String["--tuf-db"])
	if err != nil {
		log.Error("error creating local TUF client", "err", err)
		return err
	}
	remote, err := tuf.HTTPRemoteStore(args.String["--repository"], nil)
	if err != nil {
		log.Error("error creating remote TUF client", "err", err)
		return err
	}
	client := tuf.NewClient(local, remote)

	log.Info("updating TUF data")
	if _, err := client.Update(); err != nil && !tuf.IsLatestSnapshot(err) {
		log.Error("error updating TUF client", "err", err)
		return err
	}

	// read the TUF db so we can pass it to hosts
	log.Info("reading TUF database")
	tufDB, err := ioutil.ReadFile(args.String["--tuf-db"])
	if err != nil {
		log.Error("error reading the TUF database", "err", err)
		return err
	}

	log.Info("getting host list")
	clusterClient, err := cluster.NewClient()
	if err != nil {
		log.Error("error creating cluster client", "err", err)
		return err
	}
	hosts, err := clusterClient.ListHosts()
	if err != nil {
		log.Error("error getting host list", "err", err)
		return err
	}
	if len(hosts) == 0 {
		return errors.New("no hosts found")
	}

	log.Info("pulling images on all hosts")
	hostErrs := make(chan error)
	for _, h := range hosts {
		go func(hostID string) {
			log := log.New("host", hostID)

			log.Info("connecting to host")
			host, err := clusterClient.DialHost(hostID)
			if err != nil {
				log.Error("error connecting to host", "err", err)
				hostErrs <- err
				return
			}

			log.Info("pulling images")
			ch := make(chan *layer.PullInfo)
			stream, err := host.PullImages(
				args.String["--repository"],
				args.String["--driver"],
				args.String["--root"],
				bytes.NewReader(tufDB),
				ch,
			)
			if err != nil {
				log.Error("error pulling images", "err", err)
				hostErrs <- err
				return
			}
			defer stream.Close()
			for info := range ch {
				log.Info("pulled image layer", "name", info.Repo, "id", info.ID, "status", info.Status)
			}
			hostErrs <- stream.Err()
		}(h.ID)
	}
	var hostErr error
	for _, h := range hosts {
		if err := <-hostErrs; err != nil {
			log.Error("error pulling images", "host", h.ID, "err", err)
			hostErr = err
			continue
		}
		log.Info("images pulled successfully", "host", h.ID)
	}
	return hostErr
}
