package cli

import (
	"fmt"
	"os"
	"runtime"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-docopt"
	tuf "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-tuf/client"
	"github.com/flynn/flynn/Godeps/_workspace/src/gopkg.in/inconshreveable/log15.v2"
	"github.com/flynn/flynn/host/downloader"
	"github.com/flynn/flynn/pinkerton"
	"github.com/flynn/flynn/pkg/version"
)

func init() {
	Register("download", runDownload, `
usage: flynn-host download [--driver=<name>] [--root=<path>] [--repository=<uri>] [--tuf-db=<path>] [--config-dir=<dir>] [--bin-dir=<dir>]

Options:
  -d --driver=<name>       image storage driver [default: aufs]
  -r --root=<path>         image storage root [default: /var/lib/docker]
  -u --repository=<uri>    TUF repository URI [default: https://dl.flynn.io/tuf]
  -t --tuf-db=<path>       local TUF file [default: /etc/flynn/tuf.db]
  -c --config-dir=<dir>    config directory [default: /etc/flynn]
  -b --bin-dir=<dir>       binary directory [default: /usr/local/bin]

Download container images and Flynn binaries from a TUF repository`)
}

func runDownload(args *docopt.Args) error {
	if err := os.MkdirAll(args.String["--root"], 0755); err != nil {
		return fmt.Errorf("error creating root dir: %s", err)
	}

	log := log15.New()

	// create a TUF client and update it
	log.Info("initializing TUF client")
	tufDB := args.String["--tuf-db"]
	local, err := tuf.FileLocalStore(tufDB)
	if err != nil {
		log.Error("error creating local TUF client", "err", err)
		return err
	}
	remote, err := tuf.HTTPRemoteStore(args.String["--repository"], tufHTTPOpts("downloader"))
	if err != nil {
		log.Error("error creating remote TUF client", "err", err)
		return err
	}
	client := tuf.NewClient(local, remote)
	if err := updateTUFClient(client); err != nil {
		log.Error("error updating TUF client", "err", err)
		return err
	}

	log.Info("downloading images")
	if err := pinkerton.PullImagesWithClient(
		client,
		args.String["--repository"],
		args.String["--driver"],
		args.String["--root"],
		version.String(),
		pinkerton.InfoPrinter(false),
	); err != nil {
		return err
	}

	d := downloader.New(client, version.String())
	log.Info(fmt.Sprintf("downloading config to %s", args.String["--config-dir"]))
	if _, err := d.DownloadConfig(args.String["--config-dir"]); err != nil {
		log.Error("error downloading config", "err", err)
		return err
	}
	log.Info(fmt.Sprintf("downloading binaries to %s", args.String["--bin-dir"]))
	if _, err := d.DownloadBinaries(args.String["--bin-dir"]); err != nil {
		log.Error("error downloading binaries", "err", err)
		return err
	}

	log.Info("download complete")
	return nil
}

func tufHTTPOpts(name string) *tuf.HTTPRemoteOptions {
	return &tuf.HTTPRemoteOptions{
		UserAgent: fmt.Sprintf("flynn-host/%s %s-%s %s", version.String(), runtime.GOOS, runtime.GOARCH, name),
	}
}

// updateTUFClient updates the given client, initializing and re-running the
// update if ErrNoRootKeys is returned.
func updateTUFClient(client *tuf.Client) error {
	_, err := client.Update()
	if err == nil || tuf.IsLatestSnapshot(err) {
		return nil
	}
	if err == tuf.ErrNoRootKeys {
		if err := client.Init(rootKeys, len(rootKeys)); err != nil {
			return err
		}
		return updateTUFClient(client)
	}
	return err
}
