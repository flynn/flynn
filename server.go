package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/flynn/go-discoverd"
	"github.com/flynn/go-etcd/etcd"
	rpc "github.com/flynn/rpcplus/comborpc"
)

type Server struct {
	*HTTPFrontend
}

func (s *Server) ListenAndServe(quit <-chan struct{}) {
	s.HTTPFrontend.Start()
	<-quit
	// TODO: unregister from service discovery
	// TODO: stop frontends gracefully
}

func main() {
	rpcAddr := flag.String("rpcaddr", ":1115", "rpc listen address")
	httpAddr := flag.String("httpaddr", ":8080", "http frontend listen address")
	httpsAddr := flag.String("httpsaddr", ":4433", "https frontend listen address")
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

	var s Server
	s.HTTPFrontend = NewHTTPFrontend(*httpAddr, *httpsAddr, etcd.NewClient(etcdAddr), d)
	rpc.Register(&Router{s})
	rpc.HandleHTTP()
	go http.ListenAndServe(*rpcAddr, nil)

	if err = d.Register("flynn-strowger-rpc", *rpcAddr); err != nil {
		log.Fatal(err)
	}

	s.ListenAndServe(nil)
}
