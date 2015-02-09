package router

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/jackc/pgx"
)

type Config json.RawMessage

func (c Config) Encode(w *pgx.WriteBuf, oid pgx.Oid) error {
	if len(c) == 0 {
		w.WriteInt32(-1)
		return nil
	}
	w.WriteInt32(int32(len(c)))
	w.WriteBytes(c)
	return nil
}

func (c Config) FormatCode() int16 {
	return pgx.TextFormatCode
}

func (c *Config) Scan(r *pgx.ValueReader) error {
	*c = Config(r.ReadBytes(r.Len()))
	return nil
}

func (c *Config) MarshalJSON() ([]byte, error) {
	if c == nil {
		return []byte("{}"), nil
	}
	return (*json.RawMessage)(c).MarshalJSON()
}

func (c *Config) UnmarshalJSON(data []byte) error {
	return (*json.RawMessage)(c).UnmarshalJSON(data)
}

type Route struct {
	ID        string `json:"id,omitempty"`
	ParentRef string `json:"parent_ref,omitempty"`
	Type      string `json:"type,omitempty"`

	Config *Config `json:"config,omitempty"`

	CreatedAt *time.Time `json:"created_at,omitempty"`
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
}

var ErrWrongType = errors.New("router: the requested route type does not match the actual type")
var ErrNoConfig = errors.New("router: the supplied route has no configuration")

func (r *Route) HTTPRoute() *HTTPRoute {
	rCopy := *r
	route := &HTTPRoute{Route: &rCopy}
	route.Route.Config = nil
	json.Unmarshal(*r.Config, route)
	return route
}

func (r *Route) TCPRoute() *TCPRoute {
	rCopy := *r
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
	Sticky  bool   `json:"sticky,omitempty"`
}

func (r *HTTPRoute) ToRoute() *Route {
	if r.Route == nil {
		r.Route = &Route{}
	}
	r.Route.Type = "http"

	rawConfig, _ := json.Marshal(r)
	route := *r.Route
	config := Config(rawConfig)
	route.Config = &config
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

	rawConfig, _ := json.Marshal(r)
	route := *r.Route
	config := Config(rawConfig)
	route.Config = &config
	return &route
}

type Event struct {
	Event string
	ID    string
	Error error
}
