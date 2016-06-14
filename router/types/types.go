package router

import (
	"encoding/json"
	"time"
)

// Certificate describes a TLS certificate for one or more routes
type Certificate struct {
	// ID is the unique ID of this Certificate
	ID string `json:"id,omitempty"`
	// Routes contains the IDs of routes assigned to this cert
	Routes []string `json:"routes,omitempty"`
	// TLSCert is the optional TLS public certificate. It is only used for HTTP routes.
	Cert string `json:"cert,omitempty"`
	// TLSCert is the optional TLS private key. It is only used for HTTP routes.
	Key string `json:"key,omitempty"`
	// CreatedAt is the time this cert was created.
	CreatedAt time.Time `json:"created_at,omitempty"`
	// UpdatedAt is the time this cert was last updated.
	UpdatedAt time.Time `json:"updated_at,omitempty"`
}

// Route is a struct that combines the fields of HTTPRoute and TCPRoute
// for easy JSON marshaling.
type Route struct {
	// Type is the type of Route, either "http" or "tcp".
	Type string `json:"type"`
	// ID is the unique ID of this route.
	ID string `json:"id,omitempty"`
	// ParentRef is an external opaque identifier used by the route creator for
	// filtering and correlation. It typically contains the app ID.
	ParentRef string `json:"parent_ref,omitempty"`
	// Service is the ID of the service.
	Service string `json:"service"`
	// Leader is whether or not traffic should only be routed to the leader or
	// all instances
	Leader bool `json:"leader"`
	// CreatedAt is the time this Route was created.
	CreatedAt time.Time `json:"created_at,omitempty"`
	// UpdatedAt is the time this Route was last updated.
	UpdatedAt time.Time `json:"updated_at,omitempty"`

	// Domain is the domain name of this Route. It is only used for HTTP routes.
	Domain string `json:"domain,omitempty"`

	// Certificate contains TLSCert and TLSKey
	Certificate *Certificate `json:"certificate,omitempty"`

	// Deprecated in favor of Certificate
	LegacyTLSCert string `json:"tls_cert,omitempty"`
	LegacyTLSKey  string `json:"tls_key,omitempty"`

	// Sticky is whether or not to use sticky sessions for this route. It is only
	// used for HTTP routes.
	Sticky bool `json:"sticky,omitempty"`
	// Path is the optional prefix to route to this service. It's exclusive with
	// the TLS options and can only be set if a "default" route with the same domain
	// and no Path already exists in the route table.
	Path string `json:"path,omitempty"`

	// Port is the TCP port to listen on for TCP Routes.
	Port int32 `json:"port,omitempty"`
}

func (r Route) FormattedID() string {
	return r.Type + "/" + r.ID
}

func (r Route) HTTPRoute() *HTTPRoute {
	return &HTTPRoute{
		ID:        r.ID,
		ParentRef: r.ParentRef,
		Service:   r.Service,
		Leader:    r.Leader,
		CreatedAt: r.CreatedAt,
		UpdatedAt: r.UpdatedAt,

		Domain:        r.Domain,
		Certificate:   r.Certificate,
		LegacyTLSCert: r.LegacyTLSCert,
		LegacyTLSKey:  r.LegacyTLSKey,
		Sticky:        r.Sticky,
		Path:          r.Path,
	}
}

func (r Route) TCPRoute() *TCPRoute {
	return &TCPRoute{
		ID:        r.ID,
		ParentRef: r.ParentRef,
		Service:   r.Service,
		Leader:    r.Leader,
		CreatedAt: r.CreatedAt,
		UpdatedAt: r.UpdatedAt,

		Port: int(r.Port),
	}
}

// HTTPRoute is an HTTP Route.
type HTTPRoute struct {
	ID        string
	ParentRef string
	Service   string
	Leader    bool
	CreatedAt time.Time
	UpdatedAt time.Time

	Domain        string
	Certificate   *Certificate `json:"certificate,omitempty"`
	LegacyTLSCert string       `json:"tls_cert,omitempty"`
	LegacyTLSKey  string       `json:"tls_key,omitempty"`
	Sticky        bool
	Path          string
}

func (r HTTPRoute) FormattedID() string {
	return "http/" + r.ID
}

func (r HTTPRoute) MarshalJSON() ([]byte, error) {
	return json.Marshal(r.ToRoute())
}

func (r HTTPRoute) ToRoute() *Route {
	return &Route{
		// common fields
		Type:      "http",
		ID:        r.ID,
		ParentRef: r.ParentRef,
		Service:   r.Service,
		Leader:    r.Leader,
		CreatedAt: r.CreatedAt,
		UpdatedAt: r.UpdatedAt,

		// http-specific fields
		Domain:        r.Domain,
		Certificate:   r.Certificate,
		LegacyTLSCert: r.LegacyTLSCert,
		LegacyTLSKey:  r.LegacyTLSKey,
		Sticky:        r.Sticky,
		Path:          r.Path,
	}
}

// TCPRoute is a TCP Route.
type TCPRoute struct {
	ID        string
	ParentRef string
	Service   string
	Leader    bool
	CreatedAt time.Time
	UpdatedAt time.Time

	Port int
}

func (r TCPRoute) FormattedID() string {
	return "tcp/" + r.ID
}

func (r TCPRoute) MarshalJSON() ([]byte, error) {
	return json.Marshal(r.ToRoute())
}

func (r TCPRoute) ToRoute() *Route {
	return &Route{
		Type:      "tcp",
		ID:        r.ID,
		ParentRef: r.ParentRef,
		Service:   r.Service,
		Leader:    r.Leader,
		CreatedAt: r.CreatedAt,
		UpdatedAt: r.UpdatedAt,

		Port: int32(r.Port),
	}
}

type Event struct {
	Event string
	ID    string
	Route *Route
	Error error
}

type StreamEvent struct {
	Event string `json:"event"`
	Route *Route `json:"route,omitempty"`
	Error error  `json:"error,omitempty"`
}
