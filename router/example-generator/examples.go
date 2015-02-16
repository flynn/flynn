package main

import (
	"io"
	"log"
	"net/http"
	"os"

	g "github.com/flynn/flynn/pkg/examplegenerator"
	"github.com/flynn/flynn/pkg/httprecorder"
	rc "github.com/flynn/flynn/router/client"
	rt "github.com/flynn/flynn/router/types"
)

type generator struct {
	client rc.Client
	route  *rt.Route
}

func main() {
	log.SetOutput(os.Stderr)

	httpClient := &http.Client{}
	client := rc.NewWithHTTP(httpClient)
	recorder := httprecorder.NewWithClient(httpClient)

	e := &generator{
		client: client,
	}

	examples := []g.Example{
		{"route_create", e.createRoute},
		{"route_update", e.updateRoute},
		{"route_list", e.listRoutes},
		{"route_get", e.getRoute},
		{"route_delete", e.deleteRoute},
	}

	var out io.Writer
	if len(os.Args) > 1 {
		var err error
		out, err = os.Create(os.Args[1])
		if err != nil {
			log.Fatal(err)
		}
	} else {
		out = os.Stdout
	}
	if err := g.WriteOutput(recorder, examples, out); err != nil {
		log.Fatal(err)
	}
}

func (e *generator) createRoute() {
	route := (&rt.HTTPRoute{
		Domain:  "http://example.com",
		Service: "foo" + "-web",
	}).ToRoute()
	err := e.client.CreateRoute(route)
	if err == nil {
		e.route = route
	}
}

func (e *generator) updateRoute() {
	route := (&rt.HTTPRoute{
		ID:      e.route.ID,
		Domain:  "http://example.com",
		Service: "bar" + "-web",
	}).ToRoute()
	e.client.UpdateRoute(route)
}

func (e *generator) listRoutes() {
	e.client.ListRoutes("")
}

func (e *generator) getRoute() {
	e.client.GetRoute(e.route.Type, e.route.ID)
}

func (e *generator) deleteRoute() {
	e.client.DeleteRoute(e.route.Type, e.route.ID)
}
