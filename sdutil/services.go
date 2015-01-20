package main

import (
	"flag"
	"fmt"
	"log"
	"time"

	"github.com/flynn/flynn/discoverd/client"
)

type instances struct {
	onlyOne *bool
}

func (cmd *instances) Name() string {
	return "instances"
}

func (cmd *instances) DefineFlags(fs *flag.FlagSet) {
	cmd.onlyOne = fs.Bool("1", false, "only show one instance")
}

func (cmd *instances) Run(fs *flag.FlagSet) {
	if fs.Arg(0) == "" {
		log.Fatal("missing service name argument")
	}
	instances, err := discoverd.GetInstances(fs.Arg(0), time.Second)
	if err != nil {
		log.Fatal(err)
	}
	if *cmd.onlyOne {
		if len(instances) > 0 {
			fmt.Println(instances[0].Addr)
		}
		return
	}
	for _, inst := range instances {
		fmt.Println(inst.Addr)
	}
}
