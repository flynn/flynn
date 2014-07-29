package arg

import (
	"flag"

	"github.com/flynn/flynn-test/cluster"
)

type Args struct {
	BootConfig   cluster.BootConfig
	CLI          string
	DockerFS     string
	Flynnrc      string
	Debug        bool
	Kill         bool
	KeepDockerFS bool
	DBPath       string
	TestsPath    string
}

func Parse() *Args {
	args := &Args{BootConfig: cluster.BootConfig{}}

	flag.StringVar(&args.BootConfig.User, "user", "ubuntu", "user to run QEMU as")
	flag.StringVar(&args.BootConfig.RootFS, "rootfs", "rootfs/rootfs.img", "fs image to use with QEMU")
	flag.StringVar(&args.BootConfig.Kernel, "kernel", "rootfs/vmlinuz", "path to the Linux binary")
	flag.StringVar(&args.BootConfig.Network, "network", "10.52.0.1/24", "the network to use for vms")
	flag.StringVar(&args.BootConfig.NatIface, "nat", "eth0", "the interface to provide NAT to vms")
	flag.StringVar(&args.DockerFS, "dockerfs", "", "docker fs")
	flag.StringVar(&args.CLI, "cli", "flynn", "path to flynn-cli binary")
	flag.StringVar(&args.Flynnrc, "flynnrc", "", "path to flynnrc file")
	flag.StringVar(&args.DBPath, "db", "flynn-test.db", "path to BoltDB database to store pending builds")
	flag.StringVar(&args.TestsPath, "tests", "flynn-test", "path to the tests binary")
	flag.BoolVar(&args.Debug, "debug", false, "enable debug output")
	flag.BoolVar(&args.Kill, "kill", true, "kill the cluster after running the tests")
	flag.BoolVar(&args.KeepDockerFS, "keep-dockerfs", false, "don't remove the dockerfs which was built to run the tests")
	flag.Parse()

	return args
}
