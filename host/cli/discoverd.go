package cli

import (
	"errors"
	"fmt"
	"log"
	"net"
	"net/url"
	"strings"

	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/cluster"
	"github.com/flynn/go-docopt"
)

func init() {
	Register("promote", runPromote, `
usage: flynn-host promote ADDR

Promotes a Flynn node to a member of the consensus cluster.
`)
	Register("demote", runDemote, `
usage: flynn-host demote [-f|--force] ADDR

Demotes a Flynn node, removing it from the consensus cluster.
`)
}

func runPromote(args *docopt.Args, client *cluster.Client) error {
	addr, err := formatAddr(args.String["ADDR"])
	if err != nil {
		return err
	}
	dd := discoverd.NewClientWithURL(addr)
	if err := dd.Promote(addr); err != nil {
		return err
	}
	log.Println("Promoted peer", addr)
	log.Println("NOTE: If you have made changes to the peer set that you intend to be permanent you should update the discoverd environment variable DISCOVERD_PEERS to reflect this.")
	return nil
}

func runDemote(args *docopt.Args, client *cluster.Client) error {
	addr, err := formatAddr(args.String["ADDR"])
	if err != nil {
		return err
	}
	force := args.Bool["--force"]
	// first try to connect to the peer and gracefully demote it
	dd := discoverd.NewClientWithURL(addr)
	err = dd.Ping(addr)
	if err == nil {
		log.Println("Attempting to gracefully demote peer.")
		err = dd.Demote(addr)
	} else if !force {
		return errors.New("Failed to contact peer to attempt graceful demotion and --force not given.")
	}
	// if that fails and --force is given forcefully remove it
	// by instructing the raft leader to remove it from the raft peers directly
	if err != nil && force {
		leader, err := discoverd.DefaultClient.RaftLeader()
		if err != nil {
			return err
		}
		dd = discoverd.NewClientWithURL(leader.Host)
		if err := dd.RaftRemovePeer(addr); err != nil {
			return err
		}
		log.Println("Forcefully removed peer", addr)
		return nil
	} else if err != nil {
		return err
	}
	log.Println("Demoted peer", addr)
	log.Println("NOTE: If you have made changes to the peer set that you intend to be permanent you should update the discoverd environment variable DISCOVERD_PEERS to reflect this.")
	return nil
}

func formatAddr(addr string) (string, error) {
	if !strings.HasPrefix(addr, "http") {
		addr = "http://" + addr
	}
	parsed, err := url.Parse(addr)
	if err != nil {
		fmt.Println("error in parsing addr err:", err)
		return "", err
	}
	host, port, _ := net.SplitHostPort(parsed.Host)
	if host == "" {
		host = parsed.Host
	}
	if port == "" {
		port = "1111"
	}
	parsed.Host = net.JoinHostPort(host, port)
	return parsed.String(), nil
}
