package cli

import (
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/docker/docker/pkg/term"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/host/resource"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/pkg/cliutil"
	"github.com/flynn/flynn/pkg/cluster"
	"github.com/flynn/flynn/pkg/exec"
	"github.com/flynn/go-docopt"
)

func init() {
	Register("run", runRun, `
usage: flynn-host run [options] [--] <artifact> <command> [<argument>...]

Run an interactive job.

Options:
	--host=<host>          run on a specific host
	--bind=<mountspecs>    bind mount a directory into the job (ex: /foo:/data,/bar:/baz)
	--volume=<path>        mount a temporary volume at <path>
	--limits=<limits>      resource limits (ex: memory=2G,temp_disk=200MB)
	--workdir=<dir>        working directory
	--hostnet              use the host network
	--profiles=<profiles>  job profiles (comma separated)
	--extra-caps=<caps>    extra Linux capabilities (comma separated)

Example:
	$ flynn-host run <(jq '.mongodb' images.json) mongo --version
	MongoDB shell version: 3.2.9
`)
}

func runRun(args *docopt.Args, client *cluster.Client) error {
	artifact := &ct.Artifact{}
	if err := cliutil.DecodeJSONArg(args.String["<artifact>"], artifact); err != nil {
		return err
	}
	cmd := exec.Cmd{
		ImageArtifact: artifact,
		Job: &host.Job{
			Config: host.ContainerConfig{
				Args:        append([]string{args.String["<command>"]}, args.All["<argument>"].([]string)...),
				TTY:         term.IsTerminal(os.Stdin.Fd()) && term.IsTerminal(os.Stdout.Fd()),
				Stdin:       true,
				DisableLog:  true,
				WorkingDir:  args.String["--workdir"],
				HostNetwork: args.Bool["--hostnet"],
			},
		},
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	}
	if hostID := args.String["--host"]; hostID != "" {
		host, err := cluster.NewClient().Host(hostID)
		if err != nil {
			return err
		}
		cmd.Host = host
	}
	if cmd.Job.Config.TTY {
		ws, err := term.GetWinsize(os.Stdin.Fd())
		if err != nil {
			return err
		}
		cmd.TermHeight = ws.Height
		cmd.TermWidth = ws.Width
		cmd.Env = map[string]string{
			"COLUMNS": strconv.Itoa(int(ws.Width)),
			"LINES":   strconv.Itoa(int(ws.Height)),
			"TERM":    os.Getenv("TERM"),
		}
	}
	if specs := args.String["--bind"]; specs != "" {
		mounts := strings.Split(specs, ",")
		cmd.Job.Config.Mounts = make([]host.Mount, len(mounts))
		for i, m := range mounts {
			s := strings.SplitN(m, ":", 2)
			cmd.Job.Config.Mounts[i] = host.Mount{
				Target:    s[0],
				Location:  s[1],
				Writeable: true,
			}
		}
	}
	if path := args.String["--volume"]; path != "" {
		cmd.Volumes = []*ct.VolumeReq{{
			Path:         path,
			DeleteOnStop: true,
		}}
	}
	if limits := args.String["--limits"]; limits != "" {
		cmd.Job.Resources = resource.Defaults()
		resources, err := resource.ParseCSV(limits)
		if err != nil {
			return err
		}
		for typ, limit := range resources {
			cmd.Job.Resources[typ] = limit
		}
	}
	if profiles := args.String["--profiles"]; profiles != "" {
		s := strings.Split(profiles, ",")
		cmd.Job.Profiles = make([]host.JobProfile, len(s))
		for i, profile := range s {
			cmd.Job.Profiles[i] = host.JobProfile(profile)
		}
	}
	if extraCaps := args.String["--extra-caps"]; extraCaps != "" {
		linuxCapabilities := append(host.DefaultCapabilities, strings.Split(extraCaps, ",")...)
		cmd.Job.Config.LinuxCapabilities = &linuxCapabilities
	}

	var termState *term.State
	if cmd.Job.Config.TTY {
		var err error
		termState, err = term.MakeRaw(os.Stdin.Fd())
		if err != nil {
			return err
		}
		// Restore the terminal if we return without calling os.Exit
		defer term.RestoreTerminal(os.Stdin.Fd(), termState)
		go func() {
			ch := make(chan os.Signal, 1)
			signal.Notify(ch, syscall.SIGWINCH)
			for range ch {
				ws, err := term.GetWinsize(os.Stdin.Fd())
				if err != nil {
					return
				}
				cmd.ResizeTTY(ws.Height, ws.Width)
				cmd.Signal(int(syscall.SIGWINCH))
			}
		}()
	}

	go func() {
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
		sig := <-ch
		cmd.Signal(int(sig.(syscall.Signal)))
		time.Sleep(10 * time.Second)
		cmd.Signal(int(syscall.SIGKILL))
	}()

	err := cmd.Run()
	if status, ok := err.(exec.ExitError); ok {
		if cmd.Job.Config.TTY {
			// The deferred restore doesn't happen due to the exit below
			term.RestoreTerminal(os.Stdin.Fd(), termState)
		}
		os.Exit(int(status))
	}
	return err
}
