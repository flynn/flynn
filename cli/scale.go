package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/controller/utils"
	"github.com/flynn/flynn/pkg/version"
	"github.com/flynn/go-docopt"
)

func init() {
	register("scale", runScale, `
usage: flynn scale [options] [<type>=<spec>...]

Scale changes the number of jobs and tags for each process type in a release.

Process type scale should be formatted like TYPE=COUNT[,KEY=VAL...], for example:

web=1                  # 1 web process
web=3                  # 3 web processes, distributed amongst all hosts
web=3,active=true      # 3 web processes, distributed amongst hosts tagged active=true
db=3,disk=ssd,mem=high # 3 db processes, distributed amongst hosts tagged with
                       # both disk=ssd and mem=high

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

// minScaleRequestVersion is the minimum API version which supports scaling
// using scale requests
const minScaleRequestVersion = "v20170121.0"

// takes args of the form "web=1[,key=val...]", "worker=3[,key=val...]", etc
func runScale(args *docopt.Args, client controller.Client) error {
	app := mustApp()

	typeSpecs := args.All["<type>=<spec>"].([]string)

	showAll := args.Bool["--all"]

	if len(typeSpecs) > 0 && showAll {
		return fmt.Errorf("ERROR: Can't use --all when scaling")
	}

	releaseID := args.String["--release"]
	if releaseID != "" && showAll {
		return fmt.Errorf("ERROR: Can't use --all in combination with --release")
	}

	if len(typeSpecs) == 0 {
		return showFormations(client, releaseID, showAll, app)
	}

	release, err := determineRelease(client, releaseID, app)
	if err != nil {
		return err
	}

	processes := make(map[string]int, len(typeSpecs))
	tags := make(map[string]map[string]string, len(typeSpecs))
	invalid := make([]string, 0, len(release.Processes))
	for _, arg := range typeSpecs {
		i := strings.IndexRune(arg, '=')
		if i < 0 {
			return fmt.Errorf("ERROR: scale args must be of the form <typ>=<spec>")
		}

		countTags := strings.Split(arg[i+1:], ",")

		count, err := strconv.Atoi(countTags[0])
		if err != nil {
			return fmt.Errorf("ERROR: could not parse quantity in %q", arg)
		} else if count < 0 {
			return fmt.Errorf("ERROR: process quantities cannot be negative in %q", arg)
		}

		processType := arg[:i]
		if _, ok := release.Processes[processType]; ok {
			processes[processType] = count
		} else {
			invalid = append(invalid, fmt.Sprintf("%q", processType))
			continue
		}

		if len(countTags) > 1 {
			processTags := make(map[string]string, len(countTags)-1)
			for i := 1; i < len(countTags); i++ {
				keyVal := strings.SplitN(countTags[i], "=", 2)
				if len(keyVal) == 1 && keyVal[0] != "" {
					processTags[keyVal[0]] = "true"
				} else if len(keyVal) == 2 {
					processTags[keyVal[0]] = keyVal[1]
				}
			}
			tags[processType] = processTags
		}
	}
	if len(invalid) > 0 {
		return fmt.Errorf("ERROR: unknown process types: %s", strings.Join(invalid, ", "))
	}

	opts := ct.ScaleOptions{
		Processes: processes,
		Tags:      tags,
		NoWait:    args.Bool["--no-wait"],
	}

	status, err := client.Status()
	if err != nil {
		return err
	}
	v := version.Parse(status.Version)
	if !v.Dev && v.Before(version.Parse(minScaleRequestVersion)) {
		return runScaleWithJobEvents(client, app, release, opts)
	}
	return runScaleWithScaleRequest(client, app, release, opts)
}

func runScaleWithScaleRequest(client controller.Client, app string, release *ct.Release, opts ct.ScaleOptions) error {
	opts.ScaleRequestCallback = func(req *ct.ScaleRequest) {
		if req.NewProcesses == nil {
			return
		}
		scale := make([]string, 0, len(release.Processes))
		for typ := range release.Processes {
			if count := (*req.NewProcesses)[typ]; count != req.OldProcesses[typ] {
				scale = append(scale, fmt.Sprintf("%s: %d=>%d", typ, req.OldProcesses[typ], count))
			}
		}
		fmt.Printf("scaling %s\n\n", strings.Join(scale, ", "))
	}
	opts.JobEventCallback = func(job *ct.Job) error {
		id := job.ID
		if id == "" {
			id = job.UUID
		}
		fmt.Printf("%s ==> %s %s %s\n", time.Now().Format("15:04:05.000"), job.Type, id, job.State)
		return nil
	}

	start := time.Now()
	if err := client.ScaleAppRelease(app, release.ID, opts); err != nil {
		return err
	}
	if !opts.NoWait {
		fmt.Printf("\nscale completed in %s\n", time.Since(start))
	}
	return nil
}

func runScaleWithJobEvents(client controller.Client, app string, release *ct.Release, opts ct.ScaleOptions) error {
	processes := opts.Processes
	tags := opts.Tags
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
		formation.Processes = make(map[string]int, len(processes))
	}
	if formation.Tags == nil {
		formation.Tags = make(map[string]map[string]string, len(tags))
	}

	currentProcs := formation.Processes
	currentTags := formation.Tags
	for k, v := range currentProcs {
		processes[k] = v
	}
	formation.Processes = processes
	formation.Tags = tags

	if scalingComplete(currentProcs, processes) {
		if !utils.FormationTagsEqual(currentTags, tags) {
			fmt.Println("persisting tag change")
			return client.PutFormation(formation)
		}
		fmt.Println("requested scale equals current scale, nothing to do!")
		return nil
	}

	scale := make([]string, 0, len(release.Processes))
	for typ := range release.Processes {
		if currentProcs[typ] != processes[typ] {
			scale = append(scale, fmt.Sprintf("%s: %d=>%d", typ, currentProcs[typ], processes[typ]))
		}
	}
	fmt.Printf("scaling %s\n\n", strings.Join(scale, ", "))

	expected := client.ExpectedScalingEvents(currentProcs, processes, release.Processes, 1)
	watcher, err := client.WatchJobEvents(app, release.ID)
	if err != nil {
		return err
	}
	defer watcher.Close()

	err = client.PutFormation(formation)
	if err != nil || opts.NoWait {
		return err
	}

	start := time.Now()
	err = watcher.WaitFor(expected, ct.DefaultScaleTimeout, func(job *ct.Job) error {
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

func showFormations(client controller.Client, releaseID string, showAll bool, app string) error {
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

func determineRelease(client controller.Client, releaseID, app string) (*ct.Release, error) {
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
