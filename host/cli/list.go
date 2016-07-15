package cli

import (
	"errors"
	"net"
	"os"
	"text/tabwriter"

	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/cluster"
	"github.com/flynn/go-docopt"
)

func init() {
	Register("list", runListHosts, `
usage: flynn-host list

Example:

  $ flynn-host list
  ID    ADDR
  host  10.0.2.15:1113

Lists ID and IP of each host`)
}

func hostRaftStatus(host *cluster.Host, peers []string, leader string) (raftStatus string) {
	raftStatus = "proxy"
	ip, _, _ := net.SplitHostPort(host.Addr())
	for _, addr := range peers {
		discIp := ip + ":1111"
		if addr == discIp {
			raftStatus = "peer"
			if leader == discIp {
				raftStatus = raftStatus + " (leader)"
			}
			break
		}
	}
	return
}

func runListHosts(args *docopt.Args) error {
	clusterClient := cluster.NewClient()
	hosts, err := clusterClient.Hosts()
	if err != nil {
		return err
	}
	if len(hosts) == 0 {
		return errors.New("no hosts found")
	}

	peers, _ := discoverd.DefaultClient.RaftPeers()
	leader, _ := discoverd.DefaultClient.RaftLeader()

	w := tabwriter.NewWriter(os.Stdout, 1, 2, 2, ' ', 0)
	defer w.Flush()
	listRec(w, "ID", "ADDR", "RAFT STATUS")
	for _, h := range hosts {
		// If we have the list of raft peers augument the output
		// with each hosts raft proxy/peer status.
		raftStatus := ""
		if len(peers) > 0 {
			raftStatus = hostRaftStatus(h, peers, leader.Host)
		}
		listRec(w, h.ID(), h.Addr(), raftStatus)
	}
	return nil
}
