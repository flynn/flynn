package main

import (
	"flag"
	"log"
	"net/http"
	"strings"

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
	var s Server
	f, err := NewHTTPFrontend(*httpAddr)
	if err != nil {
		log.Fatal(err)
	}
	s.HTTPFrontend = f
	rpc.Register(&Router{s})
	rpc.HandleHTTP()
	go http.ListenAndServe(*rpcAddr, nil)

	d, err := discoverd.NewClient()
	if err != nil {
		log.Fatal(err)
	}
	if hostPort := strings.SplitN(*rpcAddr, ":", 2); hostPort[0] != "" {
		err = d.RegisterWithHost("flynn-strowger-rpc", hostPort[0], hostPort[1], nil)
	} else {
		err = d.Register("flynn-strowger-rpc", hostPort[1], nil)
	}
	if err != nil {
		log.Fatal(err)
	}

	s.ListenAndServe(nil)
}
