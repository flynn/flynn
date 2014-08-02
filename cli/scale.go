package main

import (
	"errors"
	"strconv"
	"strings"

	"github.com/flynn/flynn-controller/client"
	ct "github.com/flynn/flynn-controller/types"
)

var cmdScale = &Command{
	Run:   runScale,
	Usage: "scale [-r <release>] <type>=<qty>...",
	Short: "change formation",
	Long: `
Scale changes the number of jobs for each process type in a release.

Example:

	$ flynn scale web=2 worker=5
`,
}

var scaleRelease string

func init() {
	cmdScale.Flag.StringVarP(&scaleRelease, "release", "r", "", "id of release to scale (defaults to current app release)")
}

// takes args of the form "web=1", "worker=3", etc
func runScale(cmd *Command, args []string, client *controller.Client) error {
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

	for _, arg := range args {
		i := strings.IndexRune(arg, '=')
		if i < 0 {
			cmd.printUsage(true)
		}
		val, err := strconv.Atoi(arg[i+1:])
		if err != nil {
			cmd.printUsage(true)
		}
		formation.Processes[arg[:i]] = val
	}

	return client.PutFormation(formation)
}
