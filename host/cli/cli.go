package cli

import (
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-docopt"
	"github.com/flynn/flynn/pkg/cluster"
	"github.com/flynn/flynn/pkg/rpcplus"
)

type command struct {
	usage string
	f     interface{}
}

var commands = make(map[string]*command)

func Register(cmd string, f interface{}, usage string) *command {
	switch f.(type) {
	case func(*docopt.Args, cluster.Host) error, func(*docopt.Args), func() error:
	default:
		panic(fmt.Sprintf("invalid command function %s '%T'", cmd, f))
	}
	c := &command{usage: strings.TrimLeftFunc(usage, unicode.IsSpace), f: f}
	commands[cmd] = c
	return c
}

var localAddr = "127.0.0.1:1113"

func Run(name string, args []string) error {
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
	case func(*docopt.Args, cluster.Host) error:
		client, err := hostClient()
		if err != nil {
			return err
		}
		return f(parsedArgs, client)
	case func(*docopt.Args):
		f(parsedArgs)
		return nil
	case func() error:
		return f()
	}

	return fmt.Errorf("unexpected command type %T", cmd.f)
}

func hostClient() (cluster.Host, error) {
	rc, err := rpcplus.DialHTTPPath("tcp", localAddr, rpcplus.DefaultRPCPath, nil)
	if err != nil {
		return nil, errors.New("error connecting to local flynn-host, is it running?")
	}
	return cluster.NewHostClient(localAddr, rc, nil), nil
}
