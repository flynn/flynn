package cli

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-docopt"
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

	var manifest map[string]string
	if err := cliutil.DecodeJSONArg(args.String["<manifest>"], &manifest); err != nil {
		return err
	}

	for image, id := range manifest {
		fmt.Printf("Downloading %s %s...\n", image, id)
		image += "?id=" + id
		cmd := exec.Command("pinkerton", "pull", "--root", args.String["--root"], "--driver", args.String["--driver"], image)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return err
		}
	}
	return nil
}
