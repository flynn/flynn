package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/docker/docker/pkg/term"
	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/pkg/cluster"
	"github.com/flynn/flynn/pkg/shutdown"
	"github.com/flynn/go-docopt"
)

func init() {
	cmd := register("run", runRun, `
usage: flynn run [-d] [-r <release>] [-e <entrypoint>] [-l] [--] <command> [<argument>...]

Run a job.

Options:
	-d, --detached    run job without connecting io streams (implies --enable-log)
	-r <release>      id of release to run (defaults to current app release)
	-e <entrypoint>   overwrite the default entrypoint of the release's image
	-l, --enable-log  send output to log streams
`)
	cmd.optsFirst = true
}

// Declared here for Windows portability
const SIGWINCH syscall.Signal = 28

func runRun(args *docopt.Args, client controller.Client) error {
	config := runConfig{
		App:        mustApp(),
		Detached:   args.Bool["--detached"],
		Release:    args.String["-r"],
		Args:       append([]string{args.String["<command>"]}, args.All["<argument>"].([]string)...),
		ReleaseEnv: true,
		Exit:       true,
		DisableLog: !args.Bool["--detached"] && !args.Bool["--enable-log"],
	}
	if config.Release == "" {
		release, err := client.GetAppRelease(config.App)
		if err == controller.ErrNotFound {
			return errors.New("No app release, specify a release with -release")
		}
		if err != nil {
			return err
		}
		if release.ImageArtifactID() == "" {
			return errors.New("App release has no image, push a release first")
		}
		config.Release = release.ID
	}
	if e := args.String["-e"]; e != "" {
		config.Entrypoint = []string{e}
	}
	return runJob(client, config)
}

type runConfig struct {
	App        string
	Detached   bool
	Release    string
	ReleaseEnv bool
	Entrypoint []string
	Args       []string
	Env        map[string]string
	Stdin      io.Reader
	Stdout     io.Writer
	Stderr     io.Writer
	DisableLog bool
	Exit       bool
}

func runJob(client controller.Client, config runConfig) error {
	req := &ct.JobRequest{
		ReleaseID: config.Release,
		Config: &ct.JobConfig{
			Cmd:        config.Args,
			TTY:        config.Stdin == nil && config.Stdout == nil && term.IsTerminal(os.Stdin.Fd()) && term.IsTerminal(os.Stdout.Fd()) && !config.Detached,
			Entrypoint: config.Entrypoint,
			Env:        config.Env,
			ReleaseEnv: config.ReleaseEnv,
			DisableLog: config.DisableLog,
		},
	}
	if config.Stdin == nil {
		config.Stdin = os.Stdin
	}
	if config.Stdout == nil {
		config.Stdout = os.Stdout
	}
	if config.Stderr == nil {
		config.Stderr = os.Stderr
	}
	if req.Config.TTY {
		if req.Config.Env == nil {
			req.Config.Env = make(map[string]string)
		}
		ws, err := term.GetWinsize(os.Stdin.Fd())
		if err != nil {
			return err
		}
		req.Config.Columns = int(ws.Width)
		req.Config.Lines = int(ws.Height)
		req.Config.Env["COLUMNS"] = strconv.Itoa(int(ws.Width))
		req.Config.Env["LINES"] = strconv.Itoa(int(ws.Height))
		req.Config.Env["TERM"] = os.Getenv("TERM")
	}

	if config.Detached {
		job, err := client.RunJobDetached(config.App, req)
		if err != nil {
			return err
		}
		log.Println(job.ID)
		return nil
	}

	rwc, err := client.RunJobAttached(config.App, req)
	if err != nil {
		return err
	}
	defer rwc.Close()
	attachClient := cluster.NewAttachClient(rwc)

	var termState *term.State
	if req.Config.TTY {
		termState, err = term.MakeRaw(os.Stdin.Fd())
		if err != nil {
			return err
		}
		// Restore the terminal if we return without calling os.Exit
		defer term.RestoreTerminal(os.Stdin.Fd(), termState)
		go func() {
			ch := make(chan os.Signal, 1)
			signal.Notify(ch, SIGWINCH)
			for range ch {
				ws, err := term.GetWinsize(os.Stdin.Fd())
				if err != nil {
					return
				}
				attachClient.ResizeTTY(ws.Height, ws.Width)
				attachClient.Signal(int(SIGWINCH))
			}
		}()
	}

	go func() {
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
		sig := <-ch
		attachClient.Signal(int(sig.(syscall.Signal)))
		time.Sleep(10 * time.Second)
		attachClient.Signal(int(syscall.SIGKILL))
	}()

	go func() {
		io.Copy(attachClient, config.Stdin)
		attachClient.CloseWrite()
	}()

	childDone := make(chan struct{})
	shutdown.BeforeExit(func() {
		<-childDone
	})
	exitStatus, err := attachClient.Receive(config.Stdout, config.Stderr)
	close(childDone)
	if err != nil {
		return err
	}
	if req.Config.TTY {
		term.RestoreTerminal(os.Stdin.Fd(), termState)
	}
	if config.Exit {
		shutdown.ExitWithCode(exitStatus)
	}
	if exitStatus != 0 {
		return RunExitError(exitStatus)
	}
	return nil
}

type RunExitError int

func (e RunExitError) Error() string {
	return fmt.Sprintf("remote job exited with status %d", e)
}
