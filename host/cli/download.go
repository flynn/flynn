package cli

import (
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-docopt"
	tuf "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-tuf/client"
	"github.com/flynn/flynn/pinkerton"
	"github.com/flynn/flynn/pkg/tufutil"
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

	// download the upstart config and image manifests
	if err := os.MkdirAll(args.String["--config-dir"], 0755); err != nil {
		return fmt.Errorf("error creating config dir: %s", err)
	}
	for _, path := range []string{"/upstart.conf", "/host-manifest.json", "/bootstrap-manifest.json"} {
		if _, err := downloadGzippedFile(client, path, args.String["--config-dir"]); err != nil {
			return err
		}
	}

	// download the init and cli binaries
	if err := os.MkdirAll(args.String["--bin-dir"], 0755); err != nil {
		return fmt.Errorf("error creating bin dir: %s", err)
	}
	for _, path := range []string{"/flynn-linux-amd64", "/flynn-init"} {
		dst, err := downloadGzippedFile(client, path, args.String["--bin-dir"])
		if err != nil {
			return err
		}
		if err := os.Chmod(dst, 0755); err != nil {
			return err
		}
	}
	return nil
}

func downloadGzippedFile(client *tuf.Client, path, dir string) (string, error) {
	file, err := tufutil.Download(client, path+".gz")
	if err != nil {
		return "", err
	}
	defer file.Close()
	dst := filepath.Join(dir, path)
	out, err := os.Create(dst)
	if err != nil {
		return "", err
	}
	gz, err := gzip.NewReader(file)
	if err != nil {
		return "", err
	}
	defer gz.Close()
	_, err = io.Copy(out, gz)
	return dst, err
}
