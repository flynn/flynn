package main

import (
	"errors"
	"io"
	"log"
	"os"
	"strconv"
	"syscall"

	"github.com/flynn/flynn-controller/client"
	ct "github.com/flynn/flynn-controller/types"
	"github.com/flynn/go-crypto-ssh/terminal"
	"github.com/flynn/go-flynn/demultiplex"
)

var (
	runDetached bool
	runRelease  string
)

var cmdRun = &Command{
	Run:   runRun,
	Usage: "run [-d] [-r <release>] <command> [<argument>...]",
	Short: "run a job",
	Long:  `Run a job`,
}

func init() {
	cmdRun.Flag.BoolVarP(&runDetached, "detached", "d", false, "run job without connecting io streams")
	cmdRun.Flag.StringVarP(&runRelease, "release", "r", "", "id of release to run (defaults to current app release)")
}

func runRun(cmd *Command, args []string, client *controller.Client) error {
	if len(args) == 0 {
		cmd.printUsage(true)
	}
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
		Cmd:       args,
		TTY:       !runDetached && terminal.IsTerminal(syscall.Stdin) && terminal.IsTerminal(syscall.Stdout),
		ReleaseID: runRelease,
	}
	if req.TTY {
		width, height, err := terminal.GetSize(syscall.Stdout)
		if err != nil {
			return err
		}
		req.Columns = width
		req.Lines = height
		req.Env = map[string]string{
			"COLUMNS": strconv.Itoa(width),
			"LINES":   strconv.Itoa(height),
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

	if req.TTY {
		state, err := terminal.MakeRaw(syscall.Stdin)
		if err != nil {
			return err
		}
		defer terminal.Restore(syscall.Stdin, state)
	}

	go func() {
		io.Copy(rwc, os.Stdin)
		rwc.CloseWrite()
	}()
	if req.TTY {
		_, err = io.Copy(os.Stdout, rwc)
	} else {
		err = demultiplex.Copy(os.Stdout, os.Stderr, rwc)
	}
	// TODO: get exit code and use it
	return err
}
