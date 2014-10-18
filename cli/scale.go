package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-docopt"
	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
)

func init() {
	register("scale", runScale, `
usage: flynn scale [-r <release>] <type>=<qty>...

Scale changes the number of jobs for each process type in a release.

Options:
	-r, --release <release>  id of release to scale (defaults to current app release)

Example:

	$ flynn scale web=2 worker=5
`)
}

// takes args of the form "web=1", "worker=3", etc
func runScale(args *docopt.Args, client *controller.Client) error {
	scaleRelease := args.String["--release"]

	if scaleRelease == "" {
		release, err := client.GetAppRelease(mustApp())
		if err == controller.ErrNotFound {
			return errors.New("No app release, specify a release with -release")
		}
		if err != nil {
			return err
		}
		scaleRelease = release.ID
	}

	formation, err := client.GetFormation(mustApp(), scaleRelease)
	if err == controller.ErrNotFound {
		formation = &ct.Formation{
			AppID:     mustApp(),
			ReleaseID: scaleRelease,
			Processes: make(map[string]int),
		}
	} else if err != nil {
		return err
	}
	if formation.Processes == nil {
		formation.Processes = make(map[string]int)
	}

	for _, arg := range args.All["<type>=<qty>"].([]string) {
		i := strings.IndexRune(arg, '=')
		if i < 0 {
			fmt.Println(commands["scale"].usage)
		}
		val, err := strconv.Atoi(arg[i+1:])
		if err != nil {
			fmt.Println(commands["scale"].usage)
		}
		formation.Processes[arg[:i]] = val
	}

	return client.PutFormation(formation)
}
