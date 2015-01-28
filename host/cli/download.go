package cli

import (
	"fmt"
	"net/url"
	"os"

	_ "github.com/flynn/flynn/Godeps/_workspace/src/github.com/docker/docker/daemon/graphdriver/aufs"
	_ "github.com/flynn/flynn/Godeps/_workspace/src/github.com/docker/docker/daemon/graphdriver/btrfs"
	_ "github.com/flynn/flynn/Godeps/_workspace/src/github.com/docker/docker/daemon/graphdriver/devmapper"
	_ "github.com/flynn/flynn/Godeps/_workspace/src/github.com/docker/docker/daemon/graphdriver/vfs"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-docopt"
	"github.com/flynn/flynn/pinkerton"
	"github.com/flynn/flynn/pkg/cliutil"
)

func init() {
	Register("download", runDownload, `
usage: flynn-host download [--driver=<name>] [--root=<path>] [<manifest>]

Options:
  -d --driver=<name>  image storage driver [default: aufs]
  -r --root=<path>    image storage root [default: /var/lib/docker]

Download container images listed in a manifest`)
}

func runDownload(args *docopt.Args) error {
	if err := os.MkdirAll(args.String["--root"], 0755); err != nil {
		return fmt.Errorf("error creating root dir: %s", err)
	}

	manifestFile := args.String["<manifest>"]
	if manifestFile == "" {
		manifestFile = "/etc/flynn/version.json"
	}

	var manifest map[string]string
	if err := cliutil.DecodeJSONArg(manifestFile, &manifest); err != nil {
		return err
	}

	ctx, err := pinkerton.BuildContext(args.String["--driver"], args.String["--root"])
	if err != nil {
		return err
	}

	for image, id := range manifest {
		parsedURL, err := url.Parse(image)
		if err != nil {
			return err
		}
		// Hide login info from printing.
		parsedURL.User = nil
		fmt.Printf("Downloading %s %s...\n", parsedURL, id)
		image += "?id=" + id
		if err := ctx.PullDocker(image, pinkerton.InfoPrinter(false)); err != nil {
			return err
		}
	}
	return nil
}
