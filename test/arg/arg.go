package arg

import (
	"flag"

	"github.com/flynn/flynn/test/cluster"
)

type Args struct {
	BootConfig  cluster.BootConfig
	CLI         string
	FlynnHost   string
	RootFS      string
	Flynnrc     string
	RouterIP    string
	Build       bool
	Debug       bool
	Stream      bool
	Kill        bool
	BuildRootFS bool
	DBPath      string
	ListenAddr  string
	TLSCert     string
	TLSKey      string
	AssetsDir   string
	Run         string
	Gist        bool
	ClusterAPI  string
	Concurrency int
}

func Parse() *Args {
	args := &Args{BootConfig: cluster.BootConfig{}}

	flag.StringVar(&args.BootConfig.User, "user", "ubuntu", "user to run QEMU as")
	flag.StringVar(&args.BootConfig.Kernel, "kernel", "rootfs/vmlinuz", "path to the Linux binary")
	flag.StringVar(&args.BootConfig.Network, "network", "10.52.0.1/24", "the network to use for vms")
	flag.StringVar(&args.BootConfig.NatIface, "nat", "eth0", "the interface to provide NAT to vms")
	flag.StringVar(&args.BootConfig.Backend, "backend", "libvirt-lxc", "the host backend to use")
	flag.StringVar(&args.RootFS, "rootfs", "rootfs/rootfs.img", "filesystem image to use with QEMU")
	flag.StringVar(&args.CLI, "cli", "flynn", "path to flynn-cli binary")
	flag.StringVar(&args.FlynnHost, "flynn-host", "../host/bin/flynn-host", "path to flynn-host binary")
	flag.StringVar(&args.Flynnrc, "flynnrc", "", "path to flynnrc file")
	flag.StringVar(&args.RouterIP, "router-ip", "127.0.0.1", "IP address of the router")
	flag.StringVar(&args.DBPath, "db", "flynn-test.db", "path to BoltDB database to store pending builds")
	flag.StringVar(&args.ListenAddr, "listen", ":443", "runner https listen address")
	flag.StringVar(&args.TLSCert, "tls-cert", "", "TLS certificate")
	flag.StringVar(&args.TLSKey, "tls-key", "", "TLS key")
	flag.StringVar(&args.AssetsDir, "assets", "runner/assets", "path to the runner assets dir")
	flag.StringVar(&args.Run, "run", "", "regular expression selecting which tests and/or suites to run")
	flag.StringVar(&args.ClusterAPI, "cluster-api", "", "cluster-api endpoint for adding and removing hosts")
	flag.BoolVar(&args.Build, "build", true, "build Flynn")
	flag.BoolVar(&args.Debug, "debug", false, "enable debug output")
	flag.BoolVar(&args.Stream, "stream", false, "stream debug output (implies --debug)")
	flag.BoolVar(&args.Kill, "kill", true, "kill the cluster after running the tests")
	flag.BoolVar(&args.BuildRootFS, "build-rootfs", false, "just build the rootfs (leaving it behind for future use) without running tests")
	flag.BoolVar(&args.Gist, "gist", false, "upload debug info to a gist")
	flag.IntVar(&args.Concurrency, "concurrency", 5, "max number of concurrent tests")
	flag.Parse()

	return args
}
