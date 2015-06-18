package main

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/flynn/flynn/pkg/etcdcluster"
)

func main() {
	externalIP := os.Getenv("EXTERNAL_IP")
	if externalIP == "" {
		log.Fatal("missing EXTERNAL_IP")
	}
	peerAddr := fmt.Sprintf("http://%s:%s", externalIP, os.Getenv("PORT_1"))
	nameSum := md5.Sum([]byte(externalIP))
	name := hex.EncodeToString(nameSum[:])

	etcd, err := exec.LookPath("etcd")
	if err != nil {
		log.Fatal(err)
	}

	args := []string{
		etcd,
		"--data-dir=/data",
		"--name=" + name,
		fmt.Sprintf("--advertise-client-urls=http://%s:%s", externalIP, os.Getenv("PORT_0")),
		"--listen-client-urls=http://0.0.0.0:" + os.Getenv("PORT_0"),
		"--initial-advertise-peer-urls=" + peerAddr,
		"--listen-peer-urls=http://0.0.0.0:" + os.Getenv("PORT_1"),
	}
	if os.Getenv("ETCD_INITIAL_CLUSTER") == "" && os.Getenv("ETCD_DISCOVERY") == "" {
		args = append(args, fmt.Sprintf("--initial-cluster=%s=%s", name, peerAddr))
	}

	if peers := os.Getenv("ETCD_INITIAL_CLUSTER"); strings.Contains(peers, peerAddr) {
		// check if etcd is running on peers, if self included and no data
		// directory, remove/add to compensate for missing data directory
		if _, err := os.Stat("/data/member/wal"); os.IsNotExist(err) {
			recreatePeer(peers, peerAddr)
		} else if err != nil {
			log.Fatal(err)
		}
	} else {
		args = append(args, "--proxy=on")
	}

	if err := syscall.Exec(etcd, args, os.Environ()); err != nil {
		log.Fatal(err)
	}
}

type peerInfo struct {
	client *etcdcluster.Client
	id     string
}

// recreatePeer compensates for the lack of a data directory for an existing
// cluster member by attempting to remove the member and re-add it.
func recreatePeer(peers, peerAddr string) {
	peerList := strings.Split(peers, ",")
	reAdd := make(chan peerInfo, len(peerList))
	var wg sync.WaitGroup
	urls := make([]string, 0, len(peerList))

	for _, peer := range peerList {
		if peer == "" {
			continue
		}
		namePeer := strings.SplitN(peer, "=", 2)
		if len(namePeer) < 2 || namePeer[1] == peerAddr {
			continue
		}
		u, err := url.Parse(namePeer[1])
		if err != nil {
			log.Printf("Malformed peer URL %q: %s", namePeer[1], err)
			return
		}
		host, _, err := net.SplitHostPort(u.Host)
		if err != nil {
			log.Printf("Malformed host/port %q: %s", u.Host, err)
			return
		}
		urls = append(urls, fmt.Sprintf("http://%s:2379", host))
	}
	// etcd sorts to pick the first member to connect to
	sort.Strings(urls)

	for _, peer := range urls {
		wg.Add(1)
		go func(peer string) {
			defer wg.Done()
			c := &etcdcluster.Client{URLs: []string{peer}}
			members, err := c.GetMembers()
			if err != nil {
				log.Printf("Error getting members from %s: %s", peer, err)
				return
			}
			os.Setenv("ETCD_INITIAL_CLUSTER_STATE", "existing")
			for _, m := range members {
				for _, mu := range m.PeerURLs {
					// ClientURLs is only populated if the peer has been
					// a member of the cluster, it will be empty if we are
					// connecting for the first time.
					if mu == peerAddr && len(m.ClientURLs) > 0 {
						reAdd <- peerInfo{c, m.ID}
						return
					}
				}
			}
		}(peer)
	}
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case info := <-reAdd:
		log.Println("peer is present in cluster but no data directory exists, removing and re-adding...")
		if err := info.client.RemoveMember(info.id); err != nil {
			log.Fatal(err)
		}
		member, err := info.client.AddMember(peerAddr)
		if err != nil {
			log.Fatal(err)
		}

		client := &etcdcluster.Client{URLs: urls}
		for {
			members, err := client.GetMembers()
			if err != nil {
				log.Fatal(err)
			}
			for _, m := range members {
				if m.ID == member.ID {
					return
				}
			}
			log.Println("self missing", members)
			time.Sleep(500 * time.Millisecond)
		}

	case <-done:
	}
}
