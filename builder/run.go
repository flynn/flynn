package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/flynn/flynn/host/types"
	"github.com/flynn/go-docopt"
)

var cmdRun = Command{
	Run: runRun,
	Usage: `
usage: flynn-build run <args>...

Run a command and generate an image layer in /out (which is expected to be a 9p
filesystem mounted by the builder)
`[1:],
}

func runRun(args *docopt.Args) error {
	// run the command
	cmdArgs := args.All["<args>"].([]string)
	var execArgs []string
	if len(cmdArgs) > 1 {
		execArgs = cmdArgs[1:]
	}
	cmd := exec.Command(cmdArgs[0], execArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("error running the command: %s", err)
	}

	// create a squashfs layer of the diff in /out
	path := "/out/layer.squashfs"
	excludes := []string{
		".container-diff",
		".container-shared",
		".containerconfig",
		".containerinit",
		"etc/hosts",
		"src",
		"out",
	}
	cmd = exec.Command("mksquashfs", host.DiffPath, path, "-noappend", "-ef", "/dev/stdin")
	cmd.Stdin = strings.NewReader(strings.Join(excludes, "\n"))
	if out, err := cmd.CombinedOutput(); err != nil {
		fmt.Fprintln(os.Stderr, string(out))
		return fmt.Errorf("error running mksquashfs: %s", err)
	}
	return nil
}
