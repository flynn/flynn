package cli

import (
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/docker/docker/pkg/term"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-docopt"
	"github.com/flynn/flynn/pkg/cluster"
	"github.com/flynn/flynn/pkg/exec"
)

func init() {
	Register("run", runRun, `
usage: flynn-host run [options] [--] <image> <command> [<argument>...]

Run an interactive job.

Options:
	--host, -h  <host>  run on a specific host
`)
}

func runRun(args *docopt.Args, client *cluster.Client) error {
	cmd := exec.Cmd{
		HostID:     args.String["--host"],
		TTY:        term.IsTerminal(os.Stdin.Fd()) && term.IsTerminal(os.Stdout.Fd()),
		Entrypoint: []string{args.String["<command>"]},
		Cmd:        args.All["<argument>"].([]string),
		Stdin:      os.Stdin,
		Stdout:     os.Stdout,
		Stderr:     os.Stderr,
		Artifact:   exec.DockerImage(args.String["<image>"]),
	}
	if cmd.TTY {
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

	var termState *term.State
	if cmd.TTY {
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
		if cmd.TTY {
			// The deferred restore doesn't happen due to the exit below
			term.RestoreTerminal(os.Stdin.Fd(), termState)
		}
		os.Exit(int(status))
	}
	return err
}
