package cli

import (
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strings"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-docopt"
	"github.com/flynn/flynn/host/config"
	"github.com/flynn/flynn/pkg/etcdcluster"
)

func init() {
	Register("init", runInit, `
usage: flynn-host init [options]

options:
  --init-discovery=N  create a discovery token with an initial cluster size of N
  --discovery=TOKEN   enable cluster discovery with token
  --peers=PEERS       comma-separated list of initial cluster peer IPs
  --join              join an existing cluster
  --external=IP       external IP address of host, defaults to the first IPv4 address of eth0
  --no-consensus      don't participate in cluster consensus
  --file=NAME         file to write to [default: /etc/flynn/host.json]
  `)
}

func runInit(args *docopt.Args) error {
	discoveryToken := args.String["--discovery"]
	if n, ok := args.String["--init-discovery"]; ok {
		if n == "1" {
			return errors.New("There is no need for a discovery token when starting a single node cluster.")
		}
		var err error
		discoveryToken, err = etcdcluster.NewDiscoveryToken(n)
		if err != nil {
			return err
		}
		fmt.Println(discoveryToken)
	}

	ip := args.String["--external"]
	if ip == "" {
		var err error
		ip, err = config.DefaultExternalIP()
		if err != nil {
			return err
		}
	}

	var peers []string
	if s, ok := args.String["--peers"]; ok {
		peers = strings.Split(s, ",")
	}

	if args.Bool["--join"] && !args.Bool["--no-consensus"] {
		if len(peers) == 0 && discoveryToken == "" {
			return errors.New("--peers or --discovery must be specified with --join")
		}

		self := fmt.Sprintf("http://%s:2380", ip)
		client := &etcdcluster.Client{}

		if discoveryToken != "" {
			members, err := etcdcluster.Discover(discoveryToken)
			if err != nil {
				return err
			}
			if len(members) == 0 {
				return errors.New("no peers found via discovery")
			}
			client.URLs = make([]string, len(members))
			for i, m := range members {
				// replace peer port with default client API port
				u, err := url.Parse(m.PeerURLs[0])
				if err != nil {
					return err
				}
				host, _, _ := net.SplitHostPort(u.Host)
				u.Host = net.JoinHostPort(host, "2379")
				client.URLs[i] = u.String()
			}
		} else {
			client.URLs = make([]string, len(peers))
			for i, p := range peers {
				client.URLs[i] = fmt.Sprintf("http://%s:2379", p)
			}

			members, err := client.GetMembers()
			if err != nil {
				return err
			}

			peers = peers[:0]
			for _, m := range members {
				peers = append(peers, fmt.Sprintf("%s=%s", m.Name, m.PeerURLs[0]))
			}
			peers = append(peers, fmt.Sprintf("%s=%s", peerName(ip), self))
		}

		if err := client.AddMember(self); err != nil {
			return err
		}
	} else {
		// dedup peers including this node
		peerMap := make(map[string]struct{}, len(peers))
		for _, p := range peers {
			peerMap[p] = struct{}{}
		}
		if !args.Bool["--no-consensus"] {
			peerMap[ip] = struct{}{}
		}
		peers = peers[:0]
		for ip := range peerMap {
			peers = append(peers, fmt.Sprintf("%s=http://%s:2380", peerName(ip), ip))
		}
	}
	sort.Strings(peers)

	c := config.New()
	c.Env["ETCD_NAME"] = peerName(ip)
	if discoveryToken == "" {
		c.Env["ETCD_INITIAL_CLUSTER"] = strings.Join(peers, ",")
	} else {
		c.Env["ETCD_DISCOVERY"] = discoveryToken
	}
	if args.Bool["--no-consensus"] {
		c.Env["ETCD_PROXY"] = "on"
	} else if args.Bool["--join"] {
		// etcd doesn't like this being set if a discovery token is used
		if discoveryToken == "" {
			c.Env["ETCD_INITIAL_CLUSTER_STATE"] = "existing"
		}
	} else {
		c.Env["ETCD_INITIAL_CLUSTER_STATE"] = "new"
	}

	return c.WriteTo(args.String["--file"])
}

func peerName(ip string) string {
	hash := md5.Sum([]byte(ip))
	return hex.EncodeToString(hash[:])
}
