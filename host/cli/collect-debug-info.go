package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/cluster"
	"github.com/flynn/flynn/pkg/sirenia/state"
	"github.com/flynn/go-docopt"
	"github.com/inconshreveable/log15"
)

var flynnHostLogs = map[string]string{
	// the following two entries are legacy paths from when flynn-host used
	// to log to stdout (which would be redirected to these files)
	"upstart-flynn-host.log": "/var/log/upstart/flynn-host.log",
	"tmp-flynn-host.log":     "/tmp/flynn-host.log",
}

var debugCmds = [][]string{
	{"ps", "faux"},
	{"ifconfig"},
	{"uname", "-a"},
	{"lsb_release", "-a"},
	{"date"},
	{"free", "-m"},
	{"df", "-h"},
	{os.Args[0], "version"},
	{"route", "-n"},
	{"iptables", "-L", "-v", "-n", "--line-numbers"},
}

func init() {
	Register("collect-debug-info", runCollectDebugInfo, `
usage: flynn-host collect-debug-info [options]

Options:
  --tarball          Create a tarball instead of uploading to a gist
  --include-env      Include sensitive environment variables
  --log-dir=DIR      Path to the log directory [default: /var/log/flynn]
  --filename=PATH    Path to write tarball, only valid if --tarball is specified

Collect debug information into an anonymous gist or tarball`)
}

func runCollectDebugInfo(args *docopt.Args) error {
	log := log15.New()
	if args.Bool["--tarball"] {
		log.Info("creating a tarball containing logs and debug information")
	} else {
		log.Info("uploading logs and debug information to a private, anonymous gist")
	}
	log.Info("this may take a while depending on the size of your logs")

	gist := &Gist{
		Description: "Flynn debug information",
		Public:      false,
		Files:       make(map[string]File),
	}

	log.Info("getting flynn-host logs")
	logDir := args.String["--log-dir"]
	flynnHostLogs["flynn-host.log"] = filepath.Join(logDir, "flynn-host.log")
	for name, filepath := range flynnHostLogs {
		if err := gist.AddLocalFile(name, filepath); err != nil && !os.IsNotExist(err) {
			log.Error(fmt.Sprintf("error getting flynn-host log %q", filepath), "err", err)
		}
	}

	log.Info("getting sirenia metadata")
	if err := captureSireniaMetadata(gist); err != nil {
		log.Error("error getting sirenia metadata", "err", err)
	}

	log.Info("getting scheduler state")
	if err := captureSchedulerState(gist); err != nil {
		log.Error("error getting scheduler state", "err", err)
	}

	log.Info("getting job logs")
	if err := captureJobs(gist, args.Bool["--include-env"]); err != nil {
		log.Error("error getting job logs, falling back to on-disk logs", "err", err)
		debugCmds = append(debugCmds, []string{"bash", "-c", fmt.Sprintf("tail -n+1 %s/*.log", logDir)})
	}

	log.Info("getting system information")
	var debugOutput string
	for _, cmd := range debugCmds {
		output, err := captureCmd(cmd[0], cmd[1:]...)
		if err != nil {
			log.Error(fmt.Sprintf("error capturing output of %q", strings.Join(cmd, " ")), "err", err)
			continue
		}
		debugOutput += fmt.Sprintln("===>", strings.Replace(strings.Join(cmd, " "), "\n", `\n`, -1), "\n", output)
	}
	gist.AddFile("0-debug-output.log", debugOutput)

	if gist.Size > GistMaxSize {
		log.Info(fmt.Sprintf("Total size of %d bytes exceeds gist limit, falling back to tarball.", gist.Size))
	}

	if args.Bool["--tarball"] || gist.Size > GistMaxSize {
		path := args.String["--filename"]
		if path == "" {
			tmpDir, err := ioutil.TempDir("", "flynn-host-debug")
			if err != nil {
				return err
			}
			path = filepath.Join(tmpDir, "flynn-host-debug.tar.gz")
		}
		err := gist.CreateTarball(path)
		if err != nil {
			log.Error("error creating tarball", "err", err)
			return err
		}
		log.Info(fmt.Sprintf("created tarball containing debug information at %s", path))
		return nil
	}

	if err := gist.Upload(log); err != nil {
		return err
	}

	log.Info(fmt.Sprintf("debug information uploaded to: %s", gist.URL))
	return nil
}

func captureCmd(name string, arg ...string) (string, error) {
	var buf bytes.Buffer
	c := exec.Command(name, arg...)
	c.Stdout = &buf
	if err := c.Run(); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func captureJobs(gist *Gist, env bool) error {
	client := cluster.NewClient()

	jobs, err := jobList(client, true)
	if err != nil {
		return err
	}

	var buf bytes.Buffer
	printJobs(jobs, &buf)
	gist.AddFile("1-jobs.log", buf.String())

	for _, job := range jobs {
		var name string
		if system, ok := job.Job.Metadata["flynn-system-app"]; !ok || system != "true" {
			continue // Skip non-system applications
		}
		if app, ok := job.Job.Metadata["flynn-controller.app_name"]; ok {
			name += app + "-"
		}
		if typ, ok := job.Job.Metadata["flynn-controller.type"]; ok {
			name += typ + "-"
		}
		name += job.Job.ID + ".log"

		var content bytes.Buffer
		printJobDesc(&job, &content, env, nil)
		fmt.Fprint(&content, "\n\n***** ***** ***** ***** ***** ***** ***** ***** ***** *****\n\n")
		getLog(job.HostID, job.Job.ID, client, false, true, &content, &content)
		gist.AddFile(name, content.String())
	}

	return nil
}

func captureSchedulerState(gist *Gist) error {
	leader, err := discoverd.NewService("controller-scheduler").Leader()
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 10 * time.Second}
	res, err := client.Get(fmt.Sprintf("http://%s/debug/state", leader.Addr))
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected HTTP status: %s", res.Status)
	}
	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return err
	}
	gist.AddFile("scheduler-state.json", string(body))
	return nil
}

func captureSireniaMetadata(gist *Gist) error {
	appliances := []string{"postgres", "mariadb", "mongodb"}
	for _, appliance := range appliances {
		meta, err := discoverd.NewService(appliance).GetMeta()
		if err != nil {
			continue // skip if no service, appliance may not be enabled
		}
		dstate := &state.DiscoverdState{
			Index: meta.Index,
			State: &state.State{},
		}
		if len(meta.Data) > 0 {
			err := json.Unmarshal(meta.Data, dstate.State)
			if err != nil {
				continue
			}
			body, err := json.MarshalIndent(dstate, "", "    ")
			if err != nil {
				continue
			}
			gist.AddFile(fmt.Sprintf("%s-sirenia-state.json", appliance), string(body))
		}
	}
	return nil
}
