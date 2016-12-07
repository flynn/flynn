package main

import (
	"fmt"
	"log"
	"os"

	"github.com/flynn/go-docopt"
)

var usage = `
usage: flynn-build <command> [<args>...]

Commands:
  layer      build a Flynn image layer
  artifact   build a Flynn image artifact
  run        run a command and generate an image layer
`[1:]

type Command struct {
	Run   func(args *docopt.Args) error
	Usage string
}

func main() {
	args, _ := docopt.Parse(usage, nil, true, "", true)

	name := args.String["<command>"]
	var cmd Command
	switch name {
	case "layer":
		cmd = cmdLayer
	case "artifact":
		cmd = cmdArtifact
	case "run":
		cmd = cmdRun
	default:
		fmt.Fprintln(os.Stderr, usage)
		log.Fatalf("unknown command %q", name)
	}

	args, _ = docopt.Parse(cmd.Usage, append([]string{name}, args.All["<args>"].([]string)...), true, "", true)
	if err := cmd.Run(args); err != nil {
		log.Fatal(err)
	}
}
