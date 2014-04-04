package strowger

import (
	"encoding/json"
	"errors"
	"time"
)

type Route struct {
	ID        string `json:"id,omitempty"`
	ParentRef string `json:"parent_ref,omitempty"`
	Type      string `json:"type,omitempty"`

	Config *json.RawMessage `json:"config,omitempty"`

	CreatedAt *time.Time `json:"created_at,omitempty"`
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
}

var ErrWrongType = errors.New("strowger: the requested route type does not match the actual type")
var ErrNoConfig = errors.New("strowger: the supplied route has no configuration")

func (r *Route) HTTPRoute() *HTTPRoute {
	rCopy := *r
	if rCopy.Config == nil {
		empty := json.RawMessage(`{}`)
		rCopy.Config = &empty
	}
	route := &HTTPRoute{Route: &rCopy}
	route.Route.Config = nil
	json.Unmarshal(*r.Config, route)
	return route
}

func (r *Route) TCPRoute() *TCPRoute {
	rCopy := *r
	if rCopy.Config == nil {
		empty := json.RawMessage(`{}`)
		rCopy.Config = &empty
	}
	route := &TCPRoute{Route: &rCopy}
	route.Route.Config = nil
	json.Unmarshal(*r.Config, route)
	return route
}

type HTTPRoute struct {
	*Route  `json:"-"`
	Domain  string `json:"domain,omitempty"`
	Service string `json:"service,omitempty"`
	TLSCert string `json:"tls_cert,omitempty"`
	TLSKey  string `json:"tls_key,omitempty"`
}

func (r *HTTPRoute) ToRoute() *Route {
	if r.Route == nil {
		r.Route = &Route{}
	}
	r.Route.Type = "http"

	config, _ := json.Marshal(r)
	jsonConfig := json.RawMessage(config)
	route := *r.Route
	route.Config = &jsonConfig
	return &route
}

type TCPRoute struct {
	*Route  `json:"-"`
	Port    int    `json:"port"`
	Service string `json:"service"`
}

func (r *TCPRoute) ToRoute() *Route {
	if r.Route == nil {
		r.Route = &Route{}
	}
	r.Route.Type = "tcp"

	config, _ := json.Marshal(r)
	jsonConfig := json.RawMessage(config)
	route := *r.Route
	route.Config = &jsonConfig
	return &route
}

type Event struct {
	Event string
	ID    string
	Error error
}
