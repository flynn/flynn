package main

import (
	"flag"
	"log"
	"net/http"

	"github.com/flynn/rpcplus"
)

type Server struct {
	*HTTPFrontend
}

func (s *Server) ListenAndServe(quit <-chan struct{}) {
	// TODO: join service discovery
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
	rpcplus.Register(&Router{s})
	rpcplus.HandleHTTP()
	go http.ListenAndServe(*rpcAddr, nil)
	s.ListenAndServe(nil)
}
