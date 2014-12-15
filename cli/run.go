package main

import (
	"errors"
	"io"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/docker/docker/pkg/term"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-docopt"
	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/pkg/cluster"
)

func init() {
	cmd := register("run", runRun, `
usage: flynn run [-d] [-r <release>] [-e <entrypoint>] [--] <command> [<argument>...]

Run a job.

Options:
	-d, --detached   run job without connecting io streams
	-r <release>     id of release to run (defaults to current app release)
	-e <entrypoint>  overwrite the default entrypoint of the release's image
`)
	cmd.optsFirst = true
}

// Declared here for Windows portability
const SIGWINCH syscall.Signal = 28

func runRun(args *docopt.Args, client *controller.Client) error {
	runDetached := args.Bool["--detached"]
	runRelease := args.String["-r"]

	if runRelease == "" {
		release, err := client.GetAppRelease(mustApp())
		if err == controller.ErrNotFound {
			return errors.New("No app release, specify a release with -release")
		}
		if err != nil {
			return err
		}
		runRelease = release.ID
	}
	req := &ct.NewJob{
		Cmd:       append([]string{args.String["<command>"]}, args.All["<argument>"].([]string)...),
		TTY:       term.IsTerminal(os.Stdin.Fd()) && term.IsTerminal(os.Stdout.Fd()) && !runDetached,
		ReleaseID: runRelease,
	}
	if args.String["-e"] != "" {
		req.Entrypoint = []string{args.String["-e"]}
	}
	if req.TTY {
		ws, err := term.GetWinsize(os.Stdin.Fd())
		if err != nil {
			return err
		}
		req.Columns = int(ws.Width)
		req.Lines = int(ws.Height)
		req.Env = map[string]string{
			"COLUMNS": strconv.Itoa(int(ws.Width)),
			"LINES":   strconv.Itoa(int(ws.Height)),
			"TERM":    os.Getenv("TERM"),
		}
	}

	if runDetached {
		job, err := client.RunJobDetached(mustApp(), req)
		if err != nil {
			return err
		}
		log.Println(job.ID)
		return nil
	}

	rwc, err := client.RunJobAttached(mustApp(), req)
	if err != nil {
		return err
	}
	defer rwc.Close()
	attachClient := cluster.NewAttachClient(rwc)

	var termState *term.State
	if req.TTY {
		termState, err = term.MakeRaw(os.Stdin.Fd())
		if err != nil {
			return err
		}
		// Restore the terminal if we return without calling os.Exit
		defer term.RestoreTerminal(os.Stdin.Fd(), termState)
		go func() {
			ch := make(chan os.Signal)
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
		ch := make(chan os.Signal)
		signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
		sig := <-ch
		attachClient.Signal(int(sig.(syscall.Signal)))
		time.Sleep(10 * time.Second)
		attachClient.Signal(int(syscall.SIGKILL))
	}()
	go func() {
		io.Copy(attachClient, os.Stdin)
		attachClient.CloseWrite()
	}()
	exitStatus, err := attachClient.Receive(os.Stdout, os.Stderr)
	if err != nil {
		return err
	}
	if req.TTY {
		term.RestoreTerminal(os.Stdin.Fd(), termState)
	}
	os.Exit(exitStatus)

	panic("unreached")
}
