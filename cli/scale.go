package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-docopt"
	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
)

func init() {
	register("scale", runScale, `
usage: flynn scale [options] [<type>=<qty>...]

Scale changes the number of jobs for each process type in a release.

Ommitting the arguments will show the current scale.

Options:
	-n, --no-wait            don't wait for the scaling events to happen
	-r, --release=<release>  id of release to scale (defaults to current app release)
	-a, --all                show non-zero formations from all releases (only works when listing formations, can't be combined with --release)

Example:

	$ flynn scale
	web=4 worker=2

	$ flynn scale --all
	496d6e74-9db9-4cff-bcce-a3b44015907a (current)
	web=1 worker=2

	632cd907-85ab-4e53-90d0-84635650ec9a
	web=2

	$ flynn scale web=2 worker=5
	scaling web: 4=>2, worker: 2=>5

	02:28:34.333 ==> web flynn-3f656af6f1e44092aa7037046236b203 down
	02:28:34.466 ==> web flynn-ee83def0b8e4455793a43c8c70f5b34e down
	02:28:35.479 ==> worker flynn-84f70ca18c9641ef83a178a19db867a3 up
	02:28:36.508 ==> worker flynn-a3de8c326cc542aa89235e53ba304260 up
	02:28:37.601 ==> worker flynn-e24760c511af4733b01ed5b98aa54647 up

	scale completed in 3.944629056s
`)
}

const scaleTimeout = 20 * time.Second

// takes args of the form "web=1", "worker=3", etc
func runScale(args *docopt.Args, client *controller.Client) error {
	app := mustApp()

	typeCounts := args.All["<type>=<qty>"].([]string)

	showAll := args.Bool["--all"]

	if len(typeCounts) > 0 && showAll {
		return fmt.Errorf("ERROR: Can't use --all when scaling")
	}

	releaseID := args.String["--release"]
	if releaseID != "" && showAll {
		return fmt.Errorf("ERROR: Can't use --all in combination with --release")
	}

	if len(typeCounts) == 0 {
		return showFormations(client, releaseID, showAll, app)
	}

	release, err := determineRelease(client, releaseID, app)
	if err != nil {
		return err
	}

	formation, err := client.GetFormation(app, release.ID)
	if err == controller.ErrNotFound {
		formation = &ct.Formation{
			AppID:     app,
			ReleaseID: release.ID,
			Processes: make(map[string]int),
		}
	} else if err != nil {
		return err
	}
	if formation.Processes == nil {
		formation.Processes = make(map[string]int)
	}

	current := formation.Processes
	processes := make(map[string]int, len(current)+len(typeCounts))
	for k, v := range current {
		processes[k] = v
	}
	invalid := make([]string, 0, len(release.Processes))
	for _, arg := range typeCounts {
		i := strings.IndexRune(arg, '=')
		if i < 0 {
			return fmt.Errorf("ERROR: scale args must be of the form <typ>=<qty>")
		}
		val, err := strconv.Atoi(arg[i+1:])
		if err != nil {
			return fmt.Errorf("ERROR: could not parse quantity in %q", arg)
		} else if val < 0 {
			return fmt.Errorf("ERROR: process quantities cannot be negative in %q", arg)
		}
		processType := arg[:i]
		if _, ok := release.Processes[processType]; ok {
			processes[processType] = val
		} else {
			invalid = append(invalid, fmt.Sprintf("%q", processType))
		}
	}
	if len(invalid) > 0 {
		return fmt.Errorf("ERROR: unknown process types: %s", strings.Join(invalid, ", "))
	}
	formation.Processes = processes

	if scalingComplete(current, processes) {
		fmt.Println("requested scale equals current scale, nothing to do!")
		return nil
	}

	scale := make([]string, 0, len(release.Processes))
	for typ := range release.Processes {
		if current[typ] != processes[typ] {
			scale = append(scale, fmt.Sprintf("%s: %d=>%d", typ, current[typ], processes[typ]))
		}
	}
	fmt.Printf("scaling %s\n\n", strings.Join(scale, ", "))

	expected := client.ExpectedScalingEvents(current, processes, release.Processes, 1)
	watcher, err := client.WatchJobEvents(app, release.ID)
	if err != nil {
		return err
	}
	defer watcher.Close()

	err = client.PutFormation(formation)
	if err != nil || args.Bool["--no-wait"] {
		return err
	}

	start := time.Now()
	err = watcher.WaitFor(expected, scaleTimeout, func(job *ct.Job) error {
		id := job.ID
		if id == "" {
			id = job.UUID
		}
		fmt.Printf("%s ==> %s %s %s\n", time.Now().Format("15:04:05.000"), job.Type, id, job.State)
		return nil
	})

	if err != nil {
		return err
	}
	fmt.Printf("\nscale completed in %s\n", time.Since(start))
	return nil
}

func showFormations(client *controller.Client, releaseID string, showAll bool, app string) error {
	release, err := determineRelease(client, releaseID, app)
	if err != nil {
		return err
	}
	var releases []*ct.Release
	if showAll {
		var err error
		releases, err = client.AppReleaseList(app)
		if err != nil {
			return err
		}
	} else {
		releases = []*ct.Release{release}
	}

	formations := make(map[string]*ct.Formation, len(releases))
	for _, r := range releases {
		formation, err := client.GetFormation(app, r.ID)
		if err != nil && err != controller.ErrNotFound {
			return err
		}
		formations[r.ID] = formation
	}

	for i, r := range releases {
		f := formations[r.ID]
		if f == nil || len(f.Processes) == 0 {
			continue
		}
		if showAll {
			if i > 0 {
				fmt.Println()
			}
			var suffix string
			if r.ID == release.ID {
				suffix = " (current)"
			}
			fmt.Printf("%s%s\n", r.ID, suffix)
		}
		scale := make([]string, 0, len(r.Processes))
		for typ := range r.Processes {
			n := f.Processes[typ]
			if showAll && n == 0 {
				continue
			}
			scale = append(scale, fmt.Sprintf("%s=%d", typ, n))
		}
		fmt.Println(strings.Join(scale, " "))
	}
	return nil
}

func determineRelease(client *controller.Client, releaseID, app string) (*ct.Release, error) {
	if releaseID == "" {
		release, err := client.GetAppRelease(app)
		if err == controller.ErrNotFound {
			return nil, errors.New("No app release, specify a release with --release")
		}
		if err != nil {
			return nil, err
		}
		return release, nil
	}
	return client.GetRelease(releaseID)
}

func scalingComplete(actual, expected map[string]int) bool {
	// check all the expected counts are the same in actual
	for typ, count := range expected {
		if actual[typ] != count {
			return false
		}
	}
	// check any counts in actual which aren't in expected are zero
	for typ, count := range actual {
		if _, ok := expected[typ]; !ok && count != 0 {
			return false
		}
	}
	return true
}
