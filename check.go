package main

import (
	"flag"
)

type check struct {
	clientCmd
}

func (cmd *check) Name() string {
	return "check"
}

func (cmd *check) DefineFlags(fs *flag.FlagSet) {
}

func (cmd *check) Run(fs *flag.FlagSet) {
	cmd.InitClient()
}
