package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/coreos/go-etcd/etcd"
	"github.com/flynn/go-discoverd"
	"github.com/flynn/strowger/types"
)

type Listener interface {
	Start() error
	Close() error
	AddRoute(*strowger.Route) error
	RemoveRoute(id string) error
	Watcher
	DataStoreReader
}

type Router struct {
	HTTP Listener
	TCP  Listener
}

func (s *Router) ListenAndServe(quit <-chan struct{}) error {
	if err := s.HTTP.Start(); err != nil {
		return err
	}
	if err := s.TCP.Start(); err != nil {
		return err
	}
	<-quit
	// TODO: unregister from service discovery
	s.HTTP.Close()
	// TODO: wait for client connections to finish
	return nil
}

func main() {
	apiPort := os.Getenv("PORT")
	if apiPort == "" {
		apiPort = "5000"
	}

	httpAddr := flag.String("httpaddr", ":8080", "http listen address")
	httpsAddr := flag.String("httpsaddr", ":4433", "https listen address")
	tcpIP := flag.String("tcpip", "", "tcp router listen ip")
	apiAddr := flag.String("apiaddr", ":"+apiPort, "api listen address")
	flag.Parse()

	// Will use DISCOVERD environment variable
	d, err := discoverd.NewClient()
	if err != nil {
		log.Fatal(err)
	}
	if err := d.Register("strowger-api", *apiAddr); err != nil {
		log.Fatal(err)
	}

	// Read etcd addresses from ETCD
	etcdAddrs := strings.Split(os.Getenv("ETCD"), ",")
	if len(etcdAddrs) == 1 && etcdAddrs[0] == "" {
		if externalIP := os.Getenv("EXTERNAL_IP"); externalIP != "" {
			etcdAddrs = []string{fmt.Sprintf("http://%s:4001", externalIP)}
		} else {
			etcdAddrs = nil
		}
	}
	etcdc := etcd.NewClient(etcdAddrs)

	var r Router
	r.TCP = NewTCPListener(*tcpIP, 0, 0, NewEtcdDataStore(etcdc, "/strowger/tcp/"), d)
	r.HTTP = NewHTTPListener(*httpAddr, *httpsAddr, NewEtcdDataStore(etcdc, "/strowger/http/"), d)

	go func() { log.Fatal(r.ListenAndServe(nil)) }()
	log.Fatal(http.ListenAndServe(*apiAddr, apiHandler(&r)))
}
