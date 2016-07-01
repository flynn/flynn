package main

import (
	"log"
	"os"
	"runtime"

	"github.com/flynn/flynn/host/containerinit"
	"github.com/opencontainers/runc/libcontainer"
	_ "github.com/opencontainers/runc/libcontainer/nsenter"
)

func init() {
	if len(os.Args) > 1 && os.Args[1] == "libcontainer-init" {
		runtime.GOMAXPROCS(1)
		runtime.LockOSThread()
		factory, _ := libcontainer.New("", libcontainer.Cgroupfs)
		if err := factory.StartInitialization(); err != nil {
			log.Fatal(err)
		}
	}
}

func main() {
	containerinit.Main()
}
