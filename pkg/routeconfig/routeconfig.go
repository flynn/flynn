package routeconfig

import (
	"bytes"
	"context"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"text/template"

	"github.com/flynn/flynn/controller/api"
	"github.com/flynn/flynn/controller/data"
	router "github.com/flynn/flynn/router/types"
	"github.com/stripe/skycfg"
	"go.starlark.net/starlark"
)

// Load loads app routes from the given route config
func Load(in io.Reader) ([]*api.AppRoutes, error) {
	// read the route config
	data, err := ioutil.ReadAll(in)
	if err != nil {
		return nil, fmt.Errorf("error reading config file: %s", err)
	}

	// initialise a skycfg FileReader to read the route config
	r := &fileReader{config: data}

	// define skycfg globals
	globals := starlark.StringDict{
		"cert_chain_from_pem": starlark.NewBuiltin("cert_chain_from_pem", certChainFromPEM),
	}

	// load the config using skycfg
	ctx := context.Background()
	config, err := skycfg.Load(ctx, "main", skycfg.WithFileReader(r), skycfg.WithGlobals(globals))
	if err != nil {
		return nil, fmt.Errorf("error reading config file: %s", err)
	}

	// execute the config to get the app routes
	msgs, err := config.Main(ctx)
	if err != nil {
		return nil, fmt.Errorf("error parsing config file: %s", err)
	}
	appRoutes := make([]*api.AppRoutes, len(msgs))
	for i, msg := range msgs {
		v, ok := msg.(*api.AppRoutes)
		if !ok {
			return nil, fmt.Errorf("error parsing config file: expected return value %d to be api.AppRoutes, got %T", i, msg)
		}
		appRoutes[i] = v
	}
	return appRoutes, nil
}

// Generate generates route config based on the given app routes
func Generate(apps []string, appRoutes []*api.AppRoutes) ([]byte, error) {
	if len(apps) != len(appRoutes) {
		return nil, errors.New("apps and routes must have the same length")
	}
	tmplData := &Data{
		AppRoutes: make([]*AppRoutes, len(appRoutes)),
	}
	for i, app := range apps {
		routes := make([]*router.Route, len(appRoutes[i].Routes))
		for j, route := range appRoutes[i].Routes {
			routes[j] = data.ToRouterRoute(app, route)
		}
		tmplData.AppRoutes[i] = &AppRoutes{
			App:    app,
			Routes: routes,
		}
	}
	var buf bytes.Buffer
	if err := configTemplate.Execute(&buf, tmplData); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// configTemplate is the template used to generate a route config file from
// existing app routes
var configTemplate = template.Must(template.New("routes.cfg").Parse(`
# FLYNN ROUTE CONFIG
# ------------------
#
# This is a Flynn route config file that defines the list of routes that should exist for a set of
# Flynn apps.
#
# To ensure the routes defined in this file exist (and that routes not defined in this file don't
# exist), apply it by running:
#
#     flynn route config apply path/to/routes.cfg
#
# To re-generate this route config based on routes that exist for a list of apps:
#
#     flynn route config generate app1 app2 app3 > path/to/routes.cfg
#
# STRUCTURE
# ---------
#
# The file uses the Starlark configuration language (https://github.com/bazelbuild/starlark)
# and is processed using the Skycfg extension library (https://github.com/stripe/skycfg).
#
# A 'main' function must be defined that returns a list of flynn.api.v1.AppRoutes protocol messages
# that represent the routes that should exist for a set of apps.

# load the v1 config helpers
load("flynn.routeconfig.v1", "config")

# return the routes
def main(ctx):
  return config.app_routes({
    {{ range .AppRoutes -}}
    "{{ .App }}": [
      {{- range .Routes -}}
      {{- if eq .Type "http" }}
      config.http_route(
	domain = "{{ .Domain }}",
	{{- if not (eq .Path "/") }}
	path = "{{ .Path }}",
	{{- end }}
	target = config.service("{{ .Service }}"{{ if .Leader }}, leader = True{{ end }}{{ if not .DrainBackends }}, drain_backends = False{{ end }}),
	{{- if .Certificate }}
	certificate = config.static_certificate('''
{{ .Certificate.Cert }}
	'''),
	{{- end }}
	{{- if .Sticky }}
	sticky = True,
	{{- end }}
	{{- if .DisableKeepAlives }}
	disable_keep_alives = True,
	{{- end }}
      ),
      {{- end -}}
      {{- if eq .Type "tcp" }}
      config.tcp_route(
	port   = {{ .Port }},
	target = config.service("{{ .Service }}"{{ if .Leader }}, leader = True{{ end }}{{ if not .DrainBackends }}, drain_backends = False{{ end }}),
	{{- if .DisableKeepAlives }}
	disable_keep_alives = True,
	{{- end }}
      ),
      {{- end -}}
      {{- end }}
    ],
    {{- end }}
  })
`[1:]))

const v1Config = `
apiv1 = proto.package("flynn.api.v1")

def app_routes(v):
  appRoutes = []

  for appName, routes in v.items():
    appRef = "apps/{}".format(appName)

    for route in routes:
      route.parent = appRef

    appRoutes.append(apiv1.AppRoutes(app = appRef, routes = routes))

  return appRoutes

def http_route(domain, target, path = "/", certificate = None, sticky = False, disable_keep_alives = False):
  route = apiv1.Route(
    http = apiv1.Route.HTTP(
      domain = domain,
      path = path,
    ),
    service_target = target,
    disable_keep_alives = disable_keep_alives,
  )

  if certificate:
    route.http.tls = apiv1.Route.TLS(
      certificate = certificate,
    )

  if sticky:
    route.http.sticky_sessions = apiv1.Route.HTTP.StickySessions()

  return route

def tcp_route(port, target, disable_keep_alives = False):
  return apiv1.Route(
    tcp = apiv1.Route.TCP(
      port = apiv1.Route.TCPPort(
	port = port,
      ),
    ),
    service_target = target,
    disable_keep_alives = disable_keep_alives,
  )

def service(name, leader = False, drain_backends = True):
  return apiv1.Route.ServiceTarget(
    service_name = name,
    leader = leader,
    drain_backends = drain_backends,
  )

def static_certificate(chainPEM):
  return apiv1.Certificate(
    chain = cert_chain_from_pem(chainPEM),
  )

config = struct(
  app_routes         = app_routes,
  http_route         = http_route,
  tcp_route          = tcp_route,
  service            = service,
  static_certificate = static_certificate,
)
`

func certChainFromPEM(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var chainPEM string
	if err := starlark.UnpackArgs(fn.Name(), args, kwargs, "chainPEM", &chainPEM); err != nil {
		return nil, err
	}
	pemData := []byte(chainPEM)
	var chain []starlark.Value
	for {
		var block *pem.Block
		block, pemData = pem.Decode(pemData)
		if block == nil {
			break
		}
		if block.Type == "CERTIFICATE" {
			chain = append(chain, starlark.String(block.Bytes))
		}
	}
	return starlark.NewList(chain), nil
}

// Data is used to render the config template
type Data struct {
	AppRoutes []*AppRoutes
}

// AppRoutes is used to render the config template
type AppRoutes struct {
	App    string
	Routes []*router.Route
}

// fileReader implements the skycfg.FileReader to load route config
type fileReader struct {
	config []byte
}

func (f *fileReader) Resolve(ctx context.Context, name, fromPath string) (string, error) {
	return name, nil
}

func (f *fileReader) ReadFile(ctx context.Context, path string) ([]byte, error) {
	switch path {
	case "main":
		return f.config, nil
	case "flynn.routeconfig.v1":
		return []byte(v1Config), nil
	default:
		return nil, fmt.Errorf("file not found: %s", path)
	}
}
