package main

import (
	"flag"
	"log"
	"net/http"

	"github.com/coreos/go-etcd/etcd"
	"github.com/flynn/go-discoverd"
	rpc "github.com/flynn/rpcplus/comborpc"
)

type Server struct {
	*HTTPFrontend
}

func (s *Server) ListenAndServe(quit <-chan struct{}) {
	go s.HTTPFrontend.serve()
	go s.HTTPFrontend.syncDatabase()
	<-quit
	// TODO: unregister from service discovery
	// TODO: stop frontends gracefully
}

func main() {
	rpcAddr := flag.String("rpcaddr", ":1115", "rpc listen address")
	httpAddr := flag.String("httpaddr", ":8080", "http frontend listen address")
	flag.Parse()

	d, err := discoverd.NewClient()
	if err != nil {
		log.Fatal(err)
	}

	var s Server
	s.HTTPFrontend = NewHTTPFrontend(*httpAddr, etcd.NewClient(nil), d)
	rpc.Register(&Router{s})
	rpc.HandleHTTP()
	go http.ListenAndServe(*rpcAddr, nil)

	if err = d.Register("flynn-strowger-rpc", *rpcAddr); err != nil {
		log.Fatal(err)
	}

	s.ListenAndServe(nil)
}
