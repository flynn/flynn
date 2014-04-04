package main

import (
	"flag"
	"log"
	"os"
	"strings"

	"github.com/flynn/go-discoverd"
	"github.com/flynn/go-etcd/etcd"
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
	<-quit
	// TODO: unregister from service discovery
	s.HTTP.Close()
	// TODO: wait for client connections to finish
	return nil
}

func main() {
	httpAddr := flag.String("httpaddr", ":8080", "http listen address")
	httpsAddr := flag.String("httpsaddr", ":4433", "https listen address")
	flag.Parse()

	// Will use DISCOVERD environment variable
	d, err := discoverd.NewClient()
	if err != nil {
		log.Fatal(err)
	}

	// Read etcd address from ETCD
	etcdAddr := strings.Split(os.Getenv("ETCD"), ",")
	if len(etcdAddr) == 1 && etcdAddr[0] == "" {
		etcdAddr = nil
	}

	var r Router
	r.HTTP = NewHTTPListener(*httpAddr, *httpsAddr,
		NewEtcdDataStore(etcd.NewClient(etcdAddr), "/strowger/http/"), d)

	r.ListenAndServe(nil)
}
