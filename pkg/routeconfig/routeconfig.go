package routeconfig

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"regexp"
	"text/template"

	"github.com/flynn/flynn/controller/api"
	router "github.com/flynn/flynn/router/types"
	"github.com/stripe/skycfg"
)

// Load loads app routes from the given route config
func Load(routeConfig io.Reader) ([]*api.AppRoutes, error) {
	// skycfg wants to read from a single file, so create a temp file
	// to hold both the main and the supplied config
	tmp, err := ioutil.TempFile("", "flynn-route-config-")
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()

	// write the main config followed by the route config to the tmp file
	if _, err := io.WriteString(tmp, configMain); err != nil {
		return nil, fmt.Errorf("error creating config file: %s", err)
	}
	if _, err := io.Copy(tmp, routeConfig); err != nil {
		return nil, fmt.Errorf("error creating config file: %s", err)
	}

	// flush the config to disk by closing the tmp file
	if err := tmp.Close(); err != nil {
		return nil, fmt.Errorf("error creating config file: %s", err)
	}

	// load the config using skycfg
	ctx := context.Background()
	config, err := skycfg.Load(ctx, tmp.Name())
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

var multiNewlinesPattern = regexp.MustCompile("\n\n+")

// Generate generates route config based on the given data
func Generate(data *Data) ([]byte, error) {
	var buf bytes.Buffer
	if err := configTemplate.Execute(&buf, data); err != nil {
		return nil, err
	}
	// remove multiple newlines caused by the template actions
	return multiNewlinesPattern.ReplaceAll(buf.Bytes(), []byte("\n")), nil
}

// configMain is the main skycfg code that is preprended to user supplied route
// config when configuring routes
const configMain = `
api = proto.package("flynn.api.v1")

def main(ctx):
  allAppRoutes = []

  for appName, appRoutes in routes(ctx).items():
    appRef = "apps/{}".format(appName)

    for route in appRoutes:
      route.parent = appRef

    allAppRoutes.append(api.AppRoutes(app = appRef, routes = appRoutes))

  return allAppRoutes

def http_route(domain, service):
  return api.Route(
    http = api.Route.HTTP(domain = domain),
    service_target = api.Route.ServiceTarget(service_name = service, drain_backends = True),
  )

def tcp_route(port, service):
  return api.Route(
    tcp = api.Route.TCP(port = api.Route.TCPPort(port = port)),
    service_target = api.Route.ServiceTarget(service_name = service, drain_backends = True),
  )

### config read from the user's config file ends up below this line ###
`

// configTemplate is the template used to generate a route config file from
// existing app routes
var configTemplate = template.Must(template.New("routes.cfg").Parse(`
def routes(ctx):
  return {
{{ range .AppRoutes }}
    "{{ .App }}": [
{{ range .Routes }}
{{ if eq .Type "http" }}
      http_route(
        domain  = "{{ .Domain }}",
        service = "{{ .Service }}",
      ),
{{ else if eq .Type "tcp" }}
      tcp_route(
        port    = {{ .Port }},
        service = "{{ .Service }}",
      ),
{{ end }}
{{ end }}
    ],
{{ end }}
  }
`[1:]))

// Data is used to render the config template
type Data struct {
	AppRoutes []*AppRoutes
}

// AppRoutes is used to render the config template
type AppRoutes struct {
	App    string
	Routes []*router.Route
}
