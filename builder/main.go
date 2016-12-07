package main

import (
	"fmt"
	"log"
	"os"

	"github.com/flynn/go-docopt"
)

var usage = `
usage: flynn-builder <command> [<args>...]

Commands:
  build      build Flynn images
  run        run a command and generate an image layer
  export     export Flynn binaries, manifests & images to a TUF repository
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
	case "build":
		cmd = cmdBuild
	case "run":
		cmd = cmdRun
	case "export":
		cmd = cmdExport
	default:
		fmt.Fprintln(os.Stderr, usage)
		log.Fatalf("unknown command %q", name)
	}

	args, _ = docopt.Parse(cmd.Usage, append([]string{name}, args.All["<args>"].([]string)...), true, "", true)
	if err := cmd.Run(args); err != nil {
		log.Fatal(err)
	}
}
