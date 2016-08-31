package cli

import (
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/flynn/flynn/host/downloader"
	"github.com/flynn/flynn/pinkerton"
	"github.com/flynn/flynn/pkg/tufutil"
	"github.com/flynn/flynn/pkg/version"
	"github.com/flynn/go-docopt"
	tuf "github.com/flynn/go-tuf/client"
	"gopkg.in/inconshreveable/log15.v2"
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

Download container images and Flynn binaries from a TUF repository.

Set FLYNN_VERSION to download an explicit version.`)
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

	configDir := args.String["--config-dir"]

	version := os.Getenv("FLYNN_VERSION")
	if version == "" {
		version, err = getChannelVersion(configDir, client, log)
		if err != nil {
			return err
		}
	}
	log.Info(fmt.Sprintf("downloading components with version %s", version))

	log.Info("downloading images")
	if err := pinkerton.PullImagesWithClient(
		client,
		args.String["--repository"],
		args.String["--driver"],
		args.String["--root"],
		version,
		pinkerton.InfoPrinter(false),
	); err != nil {
		return err
	}

	d := downloader.New(client, version)

	log.Info(fmt.Sprintf("downloading config to %s", configDir))
	if _, err := d.DownloadConfig(configDir); err != nil {
		log.Error("error downloading config", "err", err)
		return err
	}

	binDir := args.String["--bin-dir"]
	log.Info(fmt.Sprintf("downloading binaries to %s", binDir))
	if _, err := d.DownloadBinaries(binDir); err != nil {
		log.Error("error downloading binaries", "err", err)
		return err
	}

	log.Info("download complete")
	return nil
}

func tufHTTPOpts(name string) *tuf.HTTPRemoteOptions {
	return &tuf.HTTPRemoteOptions{
		UserAgent: fmt.Sprintf("flynn-host/%s %s-%s %s", version.String(), runtime.GOOS, runtime.GOARCH, name),
		Retries:   tuf.DefaultHTTPRetries,
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

// getChannelVersion reads the locally configured release channel from
// <configDir>/channel.txt then gets the latest version for that channel
// using the TUF client.
func getChannelVersion(configDir string, client *tuf.Client, log log15.Logger) (string, error) {
	log.Info("getting configured release channel")
	data, err := ioutil.ReadFile(filepath.Join(configDir, "channel.txt"))
	if err != nil {
		log.Error("error getting configured release channel", "err", err)
		return "", err
	}
	channel := strings.TrimSpace(string(data))

	log.Info(fmt.Sprintf("determining latest version of %s release channel", channel))
	version, err := tufutil.DownloadString(client, path.Join("channels", channel))
	if err != nil {
		log.Error("error determining latest version", "err", err)
		return "", err
	}
	version = strings.TrimSpace(version)
	log.Info(fmt.Sprintf("latest %s version is %s", channel, version))
	return version, nil
}
