package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/coreos/go-etcd/etcd"
	"github.com/flynn/flynn/pkg/attempt"
)

type Config struct {
	Network   string
	EtcdURLs  string `json:",omitempty"`
	SubnetMin string `json:",omitempty"`
	SubnetMax string `json:",omitempty"`
	SubnetLen uint   `json:",omitempty"`
	Backend   struct {
		Type string
		VNI  uint `json:",omitempty"`
		Port uint `json:",omitempty"`
	}
}

var networkConfigAttempts = attempt.Strategy{
	Total: 10 * time.Minute,
	Delay: 200 * time.Millisecond,
}

func main() {
	var config Config
	config.Backend.Type = "vxlan"
	flag.StringVar(&config.Network, "network", "100.100.0.0/16", "container network")
	flag.StringVar(&config.EtcdURLs, "etcd", "http://127.0.0.1:2379", "etcd URLs")
	flag.StringVar(&config.SubnetMin, "subnet-min", "", "container network min subnet")
	flag.StringVar(&config.SubnetMax, "subnet-max", "", "container network max subnet")
	flag.UintVar(&config.SubnetLen, "subnet-len", 0, "container network subnet length")
	flag.UintVar(&config.Backend.VNI, "vni", 0, "vxlan network identifier")
	flag.UintVar(&config.Backend.Port, "port", 0, "vxlan communication port (UDP)")
	etcdKey := flag.String("key", "/coreos.com/network/config", "flannel etcd configuration key")
	flag.Parse()

	bytes, err := json.Marshal(&config)
	if err != nil {
		log.Fatal(err)
	}
	data := string(bytes)

	client := etcd.NewClient(strings.Split(config.EtcdURLs, ","))
	if err := networkConfigAttempts.Run(func() error {
		_, err = client.Create(*etcdKey, data, 0)
		if e, ok := err.(*etcd.EtcdError); ok && e.ErrorCode == 105 {
			// Skip if the key exists
			err = nil
		}
		return err
	}); err != nil {
		log.Fatal(err)
	}

	flanneld, err := exec.LookPath("flanneld")
	if err != nil {
		log.Fatal(err)
	}

	if err := syscall.Exec(
		flanneld,
		[]string{
			flanneld,
			"-etcd-endpoints=" + config.EtcdURLs,
			"-iface=" + os.Getenv("EXTERNAL_IP"),
			fmt.Sprintf("-notify-url=http://%s:1113/host/network", os.Getenv("EXTERNAL_IP")),
		},
		os.Environ(),
	); err != nil {
		log.Fatal(err)
	}
}
