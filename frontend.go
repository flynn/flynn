package main

import (
	"log"
	"net/http"

	"github.com/codegangsta/martini"
	ct "github.com/flynn/flynn-controller/types"
	strowgerc "github.com/flynn/strowger/client"
	"github.com/flynn/strowger/types"
)

func strowgerMiddleware(c martini.Context, f func() (strowgerc.Client, error), w http.ResponseWriter) {
	client, err := f()
	if err != nil {
		w.WriteHeader(500)
		log.Println(err)
	}
	c.MapTo(client, (*strowgerc.Client)(nil))

	c.Next()
	client.Close()
}

func addFrontend(frontend ct.Frontend, sc strowgerc.Client, w http.ResponseWriter) {
	if frontend.Type != "http" || frontend.Service == "" || frontend.HTTPDomain == "" {
		w.WriteHeader(400)
	}
	err := sc.AddFrontend(&strowger.Config{
		Type:       strowger.FrontendHTTP,
		Service:    frontend.Service,
		HTTPDomain: frontend.HTTPDomain,
	})
	if err != nil {
		w.WriteHeader(500)
		log.Println(err)
	}
}
