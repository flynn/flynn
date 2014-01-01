package main

import (
	"flag"
	"fmt"
	"log"
)

type services struct {
	clientCmd
	onlyOne *bool
}

func (cmd *services) Name() string {
	return "services"
}

func (cmd *services) DefineFlags(fs *flag.FlagSet) {
	cmd.onlyOne = fs.Bool("1", false, "only show one service")
}

func (cmd *services) Run(fs *flag.FlagSet) {
	cmd.InitClient(false)
	services, err := cmd.client.Services(fs.Arg(0))
	if err != nil {
		log.Fatal(err)
	}
	if *cmd.onlyOne {
		if len(services) > 0 {
			fmt.Println(services[0])
		}
		return
	}
	for _, service := range services {
		fmt.Println(service.Addr)
	}
}
