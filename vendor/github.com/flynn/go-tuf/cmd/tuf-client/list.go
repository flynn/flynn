package main

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/dustin/go-humanize"
	"github.com/flynn/go-docopt"
	tuf "github.com/flynn/go-tuf/client"
)

func init() {
	register("list", cmdList, `
usage: tuf-client list [-s|--store=<path>] <url>

Options:
  -s <path>    The path to the local file store [default: tuf.db]

List available target files.
  `)
}

func cmdList(args *docopt.Args, client *tuf.Client) error {
	if _, err := client.Update(); err != nil && !tuf.IsLatestSnapshot(err) {
		return err
	}
	targets, err := client.Targets()
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(os.Stdout, 1, 2, 2, ' ', 0)
	defer w.Flush()
	fmt.Fprintln(w, "PATH\tSIZE")
	for path, meta := range targets {
		fmt.Fprintf(w, "%s\t%s\n", path, humanize.Bytes(uint64(meta.Length)))
	}
	return nil
}
