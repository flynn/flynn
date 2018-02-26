package main

import (
	"encoding/json"
	"io"
	"os"

	"github.com/flynn/go-docopt"
	tuf "github.com/flynn/go-tuf/client"
	"github.com/flynn/go-tuf/data"
)

func init() {
	register("init", cmdInit, `
usage: tuf-client init [-s|--store=<path>] <url> [<root-keys-file>]

Options:
  -s <path>    The path to the local file store [default: tuf.db]

Initialize the local file store with root keys.
  `)
}

func cmdInit(args *docopt.Args, client *tuf.Client) error {
	file := args.String["<root-keys-file>"]
	var in io.Reader
	if file == "" || file == "-" {
		in = os.Stdin
	} else {
		var err error
		in, err = os.Open(file)
		if err != nil {
			return err
		}
	}
	var rootKeys []*data.Key
	if err := json.NewDecoder(in).Decode(&rootKeys); err != nil {
		return err
	}
	return client.Init(rootKeys, len(rootKeys))
}
