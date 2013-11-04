package main

import (
	"flag"
	"fmt"
)

type services struct {
	clientCmd
}

func (cmd *services) Name() string {
	return "services"
}

func (cmd *services) DefineFlags(fs *flag.FlagSet) {
}

func (cmd *services) Run(fs *flag.FlagSet) {
	cmd.InitClient(false)
	set := cmd.client.Services(fs.Arg(0))
	for _, addr := range set.OnlineAddrs() {
		fmt.Println(addr)
	}
}
