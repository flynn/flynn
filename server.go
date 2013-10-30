package main

import (
	"log"
	"net/http"

	"github.com/flynn/rpcplus"
)

type Server struct {
	HTTPFrontend
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
	var s Server
	f, err := NewHTTPFrontend(":8080")
	if err != nil {
		log.Fatal(err)
	}
	s.HTTPFrontend = *f
	rpcplus.Register(&Router{s})
	rpcplus.HandleHTTP()
	go http.ListenAndServe(":1115", nil)
	s.ListenAndServe(nil)
}
