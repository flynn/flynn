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
usage: flynn scale [options] <type>=<qty>...

Scale changes the number of jobs for each process type in a release.

Options:
	-n, --no-wait            don't wait for the scaling events to happen
	-r, --release <release>  id of release to scale (defaults to current app release)

Example:

	$ flynn scale web=2 worker=5
	scaling web: 0=>2, worker: 0=>5

	02:21:24.997 ==> web flynn-04093a8893d0465db8adbd98f509d011 up
	02:21:26.010 ==> web flynn-467570f5b4cb45728ddbf8d9f6b9553d up
	02:21:27.009 ==> worker flynn-0d38b2b6d964463d9a89da9654a54a8a up
	02:21:28.000 ==> worker flynn-eac9c78786a1462db3a4c2061b057849 up
	02:21:29.027 ==> worker flynn-988f2a0501654fecba18e73bd09bc891 up
	02:21:30.070 ==> worker flynn-2ee5a2957bd74363ae1398a756569fbc up
	02:21:31.154 ==> worker flynn-683470c37797464dbc7034f9ecbc695d up

	scale completed in 6.649931193s
`)
}

const scaleTimeout = 20 * time.Second

// takes args of the form "web=1", "worker=3", etc
func runScale(args *docopt.Args, client *controller.Client) error {
	app := mustApp()

	release, err := determineRelease(client, args.String["--release"], app)
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

	processes := make(map[string]int)
	for _, arg := range args.All["<type>=<qty>"].([]string) {
		i := strings.IndexRune(arg, '=')
		if i < 0 {
			fmt.Println(commands["scale"].usage)
		}
		val, err := strconv.Atoi(arg[i+1:])
		if err != nil {
			fmt.Println(commands["scale"].usage)
		}
		processes[arg[:i]] = val
	}

	current := formation.Processes
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

	stream, err := client.StreamJobEvents(app, 0)
	if err != nil {
		return err
	}
	defer stream.Close()

	err = client.PutFormation(formation)
	if err != nil || args.Bool["--no-wait"] {
		return err
	}

	start := time.Now()
loop:
	for {
		select {
		case e := <-stream.Events:
			// ignore one-off jobs or starting events
			if e.Job.State == "starting" || e.Job.Type == "" {
				continue loop
			}
			fmt.Printf("%s ==> %s %s %s\n", time.Now().Format("15:04:05.000"), e.Job.Type, e.JobID, e.Job.State)
			switch e.Job.State {
			case "up":
				current[e.Job.Type]++
			case "down", "crashed":
				current[e.Job.Type]--
			}
			if scalingComplete(current, processes) {
				fmt.Printf("\nscale completed in %s\n", time.Since(start))
				return nil
			}
		case <-time.After(scaleTimeout):
			return fmt.Errorf("timed out waiting for scale events")
		}
	}
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
