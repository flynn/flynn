package main

import (
	"fmt"
	"log"
	"strings"
	"unicode"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-docopt"
)

func init() {
	log.SetFlags(log.Lshortfile | log.Lmicroseconds)
}

func main() {
	usage := `usage: flynn-host <command> [<args>...]`

	args, _ := docopt.Parse(usage, nil, true, "", true)
	cmd := args.String["<command>"]
	cmdArgs := args.All["<args>"].([]string)

	if err := runCommand(cmd, cmdArgs); err != nil {
		log.Fatal(err)
		return
	}
}

type command struct {
	usage string
	f     interface{}
}

var commands = make(map[string]*command)

func register(cmd string, f interface{}, usage string) *command {
	switch f.(type) {
	case func(*docopt.Args) error, func(*docopt.Args):
	default:
		panic(fmt.Sprintf("invalid command function %s '%T'", cmd, f))
	}
	c := &command{usage: strings.TrimLeftFunc(usage, unicode.IsSpace), f: f}
	commands[cmd] = c
	return c
}

func runCommand(name string, args []string) error {
	argv := make([]string, 1, 1+len(args))
	argv[0] = name
	argv = append(argv, args...)

	cmd, ok := commands[name]
	if !ok {
		return fmt.Errorf("%s is not a valid command", name)
	}
	parsedArgs, err := docopt.Parse(cmd.usage, argv, true, "", false)
	if err != nil {
		return err
	}

	switch f := cmd.f.(type) {
	case func(*docopt.Args) error:
		return f(parsedArgs)
	case func(*docopt.Args):
		f(parsedArgs)
		return nil
	}

	return fmt.Errorf("unexpected command type %T", cmd.f)
}
