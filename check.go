package main

import (
	"flag"
)

type check struct {
	clientCmd
	silent *bool
}

func (cmd *check) Name() string {
	return "check"
}

func (cmd *check) DefineFlags(fs *flag.FlagSet) {
	cmd.silent = fs.Bool("s", false, "silent mode")
}

func (cmd *check) Run(fs *flag.FlagSet) {
	cmd.InitClient(*cmd.silent)
}
