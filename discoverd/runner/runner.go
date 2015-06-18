package main

import (
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/pkg/cluster"
)

func main() {
	const waitMax = time.Minute
	const waitInterval = 500 * time.Millisecond
	h := cluster.NewHost("", "http://127.0.0.1:1113", nil)

	timeout := time.After(waitMax)
	var status *host.HostStatus
	for {
		var err error
		status, err = h.GetStatus()
		if err == nil && status.Network != nil && status.Network.Subnet != "" {
			break
		}
		select {
		case <-timeout:
			if err == nil {
				err = errors.New("network didn't come up")
			}
			log.Fatal("timed out getting host status: ", err)
		default:
			time.Sleep(waitInterval)
		}
	}

	discoverd, err := exec.LookPath("discoverd")
	if err != nil {
		log.Fatal(err)
	}
	ip, _, err := net.ParseCIDR(status.Network.Subnet)
	if err != nil {
		log.Fatal(err)
	}

	if err := syscall.Exec(discoverd,
		[]string{
			discoverd,
			"-http-addr=:" + os.Getenv("PORT_0"),
			fmt.Sprintf("-dns-addr=%s:53", ip),
			"-recursors=" + strings.Join(status.Network.Resolvers, ","),
			"-notify=http://127.0.0.1:1113/host/discoverd",
		},
		os.Environ(),
	); err != nil {
		log.Fatal(err)
	}
}
