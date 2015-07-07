package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/pkg/attempt"
	"github.com/flynn/flynn/pkg/cluster"
	hh "github.com/flynn/flynn/pkg/httphelper"
)

type Config struct {
	Network   string
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

const serviceName = "flannel"

func main() {
	var config Config
	config.Backend.Type = "vxlan"
	flag.StringVar(&config.Network, "network", "100.100.0.0/16", "container network")
	flag.StringVar(&config.SubnetMin, "subnet-min", "", "container network min subnet")
	flag.StringVar(&config.SubnetMax, "subnet-max", "", "container network max subnet")
	flag.UintVar(&config.SubnetLen, "subnet-len", 0, "container network subnet length")
	flag.UintVar(&config.Backend.VNI, "vni", 0, "vxlan network identifier")
	flag.UintVar(&config.Backend.Port, "port", 0, "vxlan communication port (UDP)")
	flag.Parse()

	// wait for discoverd to come up
	status, err := cluster.WaitForHostStatus(func(status *host.HostStatus) bool {
		return status.Discoverd != nil && status.Discoverd.URL != ""
	})
	if err != nil {
		log.Fatal(err)
	}

	// create service and config if not present
	client := discoverd.NewClientWithURL(status.Discoverd.URL)
	if err := client.AddService(serviceName, nil); err != nil && !hh.IsObjectExistsError(err) {
		log.Fatalf("error creating discoverd service: %s", err)
	}
	data, err := json.Marshal(map[string]Config{"config": config})
	if err != nil {
		log.Fatal(err)
	}
	err = client.Service(serviceName).SetMeta(&discoverd.ServiceMeta{Data: data})
	if err != nil && !hh.IsObjectExistsError(err) {
		log.Fatalf("error creating discoverd service metadata: %s", err)
	}

	flanneld, err := exec.LookPath("flanneld")
	if err != nil {
		log.Fatal(err)
	}

	if err := syscall.Exec(
		flanneld,
		[]string{
			flanneld,
			"-discoverd-url=" + status.Discoverd.URL,
			"-iface=" + os.Getenv("EXTERNAL_IP"),
			fmt.Sprintf("-notify-url=http://%s:1113/host/network", os.Getenv("EXTERNAL_IP")),
		},
		os.Environ(),
	); err != nil {
		log.Fatal(err)
	}
}
