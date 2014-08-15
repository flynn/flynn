package arg

import (
	"flag"

	"github.com/flynn/flynn/test/cluster"
)

type Args struct {
	BootConfig cluster.BootConfig
	CLI        string
	RootFS     string
	Flynnrc    string
	RouterIP   string
	Build      bool
	Debug      bool
	Kill       bool
	KeepRootFS bool
	DBPath     string
	Backend    string
}

func Parse() *Args {
	args := &Args{BootConfig: cluster.BootConfig{}}

	flag.StringVar(&args.BootConfig.User, "user", "ubuntu", "user to run QEMU as")
	flag.StringVar(&args.BootConfig.Kernel, "kernel", "rootfs/vmlinuz", "path to the Linux binary")
	flag.StringVar(&args.BootConfig.Network, "network", "10.52.0.1/24", "the network to use for vms")
	flag.StringVar(&args.BootConfig.NatIface, "nat", "eth0", "the interface to provide NAT to vms")
	flag.StringVar(&args.RootFS, "rootfs", "rootfs/rootfs.img", "filesystem image to use with QEMU")
	flag.StringVar(&args.CLI, "cli", "flynn", "path to flynn-cli binary")
	flag.StringVar(&args.Flynnrc, "flynnrc", "", "path to flynnrc file")
	flag.StringVar(&args.RouterIP, "router-ip", "127.0.0.1", "IP address of the router")
	flag.StringVar(&args.DBPath, "db", "flynn-test.db", "path to BoltDB database to store pending builds")
	flag.StringVar(&args.Backend, "backend", "libvirt-lxc", "the host backend to use")
	flag.BoolVar(&args.Build, "build", true, "build Flynn")
	flag.BoolVar(&args.Debug, "debug", false, "enable debug output")
	flag.BoolVar(&args.Kill, "kill", true, "kill the cluster after running the tests")
	flag.BoolVar(&args.KeepRootFS, "keep-rootfs", false, "don't remove the rootfs which was built to run the tests")
	flag.Parse()

	return args
}
