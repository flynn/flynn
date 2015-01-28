package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-docopt"
	tuf "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-tuf/client"
	tufdata "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-tuf/data"
	"github.com/flynn/flynn/pinkerton"
	"github.com/flynn/flynn/pkg/tufutil"
)

const rootKeysJSON = `[{"keytype":"ed25519","keyval":{"public":"8c13396bf5e722fc292b3cbe5b6c4947374787e69fc4c2bb9791f060a5394e0f"}}]`

var rootKeys []*tufdata.Key

func init() {
	if err := json.Unmarshal([]byte(rootKeysJSON), &rootKeys); err != nil {
		panic("error decoding root keys")
	}

	Register("download", runDownload, `
usage: flynn-host download [--driver=<name>] [--root=<path>] [--repository=<uri>] [--tuf-db=<path>] [--manifest-dir=<dir>]

Options:
  -d --driver=<name>       image storage driver [default: aufs]
  -r --root=<path>         image storage root [default: /var/lib/docker]
  -u --repository=<uri>    image repository URI [default: https://dl.flynn.io/images]
  -t --tuf-db=<path>       local TUF file [default: /etc/flynn/tuf.db]
  -m --manifest-dir=<dir>  directory to copy manifests into [default: /etc/flynn]

Download container images from a TUF repository`)
}

func runDownload(args *docopt.Args) error {
	if err := os.MkdirAll(args.String["--root"], 0755); err != nil {
		return fmt.Errorf("error creating root dir: %s", err)
	}

	// create a TUF client, initialize it if the DB doesn't exist and update it
	tufDB := args.String["--tuf-db"]
	needsInit := false
	if _, err := os.Stat(tufDB); os.IsNotExist(err) {
		needsInit = true
	}
	local, err := tuf.FileLocalStore(tufDB)
	if err != nil {
		return err
	}
	remote, err := tuf.HTTPRemoteStore(args.String["--repository"], nil)
	if err != nil {
		return err
	}
	client := tuf.NewClient(local, remote)
	if needsInit {
		if err := client.Init(rootKeys, len(rootKeys)); err != nil {
			return err
		}
	}
	if _, err := client.Update(); err != nil && !tuf.IsLatestSnapshot(err) {
		return err
	}

	// pull images from the TUF repo
	if err := pinkerton.PullImagesWithClient(
		client,
		args.String["--repository"],
		args.String["--driver"],
		args.String["--root"],
		pinkerton.InfoPrinter(false),
	); err != nil {
		return err
	}

	// download the host and bootstrap manifests
	if err := os.MkdirAll(args.String["--manifest-dir"], 0755); err != nil {
		return fmt.Errorf("error creating manifest dir: %s", err)
	}
	for _, path := range []string{"/host-manifest.json", "/bootstrap-manifest.json"} {
		if err := downloadManifest(client, path, args.String["--manifest-dir"]); err != nil {
			return err
		}
	}
	return nil
}

func downloadManifest(client *tuf.Client, path, dir string) error {
	file, err := tufutil.Download(client, path)
	if err != nil {
		return err
	}
	defer file.Close()
	out, err := os.Create(filepath.Join(dir, path))
	if err != nil {
		return err
	}
	_, err = io.Copy(out, file)
	return err
}
