package cli

import (
	"fmt"

	"github.com/flynn/flynn/bootstrap/discovery"
	"github.com/flynn/flynn/host/config"
	"github.com/flynn/go-docopt"
)

func init() {
	Register("init", runInit, `
usage: flynn-host init [options]

options:
  --init-discovery    create and join a discovery token
  --discovery=TOKEN   join cluster with discovery token
  --peer-ips=IPLIST   join cluster using host IPs (must be already bootstrapped)
  --external-ip=IP    external IP address of host, defaults to the first IPv4 address of eth0
  --file=NAME         file to write to [default: /etc/flynn/host.json]
  `)
}

func runInit(args *docopt.Args) error {
	c := config.New()

	discoveryToken := args.String["--discovery"]
	if args.Bool["--init-discovery"] {
		var err error
		discoveryToken, err = discovery.NewToken()
		if err != nil {
			return err
		}
		fmt.Println(discoveryToken)
	}
	if discoveryToken != "" {
		c.Args = append(c.Args, "--discovery", discoveryToken)
	}
	if ip := args.String["--external-ip"]; ip != "" {
		c.Args = append(c.Args, "--external-ip", ip)
	}
	if ips := args.String["--peer-ips"]; ips != "" {
		c.Args = append(c.Args, "--peer-ips", ips)
	}

	return c.WriteTo(args.String["--file"])
}
