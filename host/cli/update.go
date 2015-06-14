package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"sync"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/docker/docker/pkg/term"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-docopt"
	tuf "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-tuf/client"
	"github.com/flynn/flynn/Godeps/_workspace/src/gopkg.in/inconshreveable/log15.v2"
	"github.com/flynn/flynn/pinkerton/layer"
	"github.com/flynn/flynn/pkg/cluster"
	"github.com/flynn/flynn/pkg/exec"
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
	remote, err := tuf.HTTPRemoteStore(args.String["--repository"], tufHTTPOpts("updater"))
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
	clusterClient := cluster.NewClient()
	hosts, err := clusterClient.Hosts()
	if err != nil {
		log.Error("error getting host list", "err", err)
		return err
	}
	if len(hosts) == 0 {
		return errors.New("no hosts found")
	}

	log.Info("pulling images on all hosts")
	images := make(map[string]string)
	var imageMtx sync.Mutex
	hostErrs := make(chan error)
	for _, h := range hosts {
		go func(host *cluster.Host) {
			log := log.New("host", host.ID())

			log.Info("connecting to host")

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
				if info.Type == layer.TypeLayer {
					continue
				}
				log.Info("pulled image", "name", info.Repo)
				imageURI := fmt.Sprintf("%s?name=%s&id=%s", args.String["--repository"], info.Repo, info.ID)
				imageMtx.Lock()
				images[info.Repo] = imageURI
				imageMtx.Unlock()
			}
			hostErrs <- stream.Err()
		}(h)
	}
	var hostErr error
	for _, h := range hosts {
		if err := <-hostErrs; err != nil {
			log.Error("error pulling images", "host", h.ID(), "err", err)
			hostErr = err
			continue
		}
		log.Info("images pulled successfully", "host", h.ID())
	}
	if hostErr != nil {
		return hostErr
	}

	updaterImage, ok := images["flynn/updater"]
	if !ok {
		e := "missing flynn/updater image"
		log.Error(e)
		return errors.New(e)
	}
	imageJSON, err := json.Marshal(images)
	if err != nil {
		log.Error("error encoding images", "err", err)
		return err
	}

	// use a flag to determine whether to use a TTY log formatter because actually
	// assigning a TTY to the job causes reading images via stdin to fail.
	cmd := exec.Command(exec.DockerImage(updaterImage), fmt.Sprintf("--tty=%t", term.IsTerminal(os.Stdout.Fd())))
	cmd.Stdin = bytes.NewReader(imageJSON)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return err
	}
	log.Info("update complete")
	return nil
}
