package routeconfig

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/flynn/flynn/controller/api"
	"github.com/flynn/flynn/router/testutils"
	router "github.com/flynn/flynn/router/types"
)

// TestLoad tests that testConfig can be loaded and returns the expected
// AppRoutes
func TestLoad(t *testing.T) {
	actual, err := Load(strings.NewReader(testConfig))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, actual, testAppRoutes)
}

// TestGenerate tests that Generate outputs a config that when loaded with
// Load returns the expected app routes
func TestGenerate(t *testing.T) {
	config, err := Generate([]string{"test1", "test2"}, testAppRoutes)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := Load(bytes.NewReader(config))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, actual, testAppRoutes)
}

// testCert is used to test configuring static certificates
var testCert = testutils.TLSConfigForDomain("test2.example.com")

// testConfig is used to test the Load function
var testConfig = `
load("flynn.routeconfig.v1", "config")

def main(ctx):
  return config.app_routes({
    "test1": [
      config.http_route(
	domain = "test1.example.com",
	target = config.service("test1-web"),
      ),
      config.http_route(
	domain = "test1.example.com",
	path   = "/foo",
	target = config.service("test1-foo"),
      ),
      config.tcp_route(
	port   = 2222,
	target = config.service("test1-ssh"),
      ),
    ],
    "test2": [
      config.http_route(
	domain      = "test2.example.com",
	target      = config.service("test2-web"),
	certificate = config.static_certificate('''` + testCert.ChainPEM() + `'''),
      ),
    ],
  })
`

// testAppRoutes are the expected app routes that result from loading
// testConfig
var testAppRoutes = []*api.AppRoutes{
	{
		App: "apps/test1",
		Routes: []*api.Route{
			api.NewRoute(router.HTTPRoute{
				Domain:  "test1.example.com",
				Service: "test1-web",
			}.ToRoute()),
			api.NewRoute(router.HTTPRoute{
				Domain:  "test1.example.com",
				Path:    "/foo",
				Service: "test1-foo",
			}.ToRoute()),
			api.NewRoute(router.TCPRoute{
				Port:    2222,
				Service: "test1-ssh",
			}.ToRoute()),
		},
	},
	{
		App: "apps/test2",
		Routes: []*api.Route{
			api.NewRoute(router.HTTPRoute{
				Domain:  "test2.example.com",
				Service: "test2-web",
				Certificate: &router.Certificate{
					Chain: testCert.Chain(),
				},
			}.ToRoute()),
		},
	},
}

// assertEqual checks that the given slices of app routes are equal
func assertEqual(t *testing.T, actual, expected []*api.AppRoutes) {
	if len(actual) != len(expected) {
		t.Fatalf("expected %d AppRoutes, got %d", len(expected), len(actual))
	}

	for i, appRoutes := range actual {
		// check the app names match
		if appRoutes.App != expected[i].App {
			t.Fatalf("expected AppRoutes[%d].App to be %s, got %s", i, expected[i].App, appRoutes.App)
		}

		// check the route counts match
		if len(appRoutes.Routes) != len(expected[i].Routes) {
			t.Fatalf("expected len(AppRoutes[%d].Routes) to be %d, got %d", i, len(appRoutes.Routes), len(expected[i].Routes))
		}

		for j, actualRoute := range appRoutes.Routes {
			// check the service matches
			if actualRoute.ServiceTarget == nil {
				t.Fatalf("expected AppRoutes[%d].Routes[%d].ServiceTarget to be set", i, j)
			}
			expectedRoute := expected[i].Routes[j].RouterType()
			if actualService := actualRoute.ServiceTarget.ServiceName; actualService != expectedRoute.Service {
				t.Fatalf("expected AppRoutes[%d].Routes[%d].ServiceTarget.ServiceName to be %s, got %s", i, j, expectedRoute.Service, actualService)
			}

			switch expectedRoute.Type {
			case "http":
				// check we have an HTTP config
				actualConfig, ok := actualRoute.Config.(*api.Route_Http)
				if !ok {
					t.Fatalf("expected AppRoutes[%d].Routes[%d].Config to be *api.Route_Http, got %T", i, j, actualRoute.Config)
				}

				// check the domain and path match
				if actualDomain := actualConfig.Http.Domain; actualDomain != expectedRoute.Domain {
					t.Fatalf("expected AppRoutes[%d].Routes[%d].Config.Http.Domain to be %s, got %s", i, j, expectedRoute.Domain, actualDomain)
				}
				if actualPath := actualConfig.Http.Path; expectedRoute.Path != "" && actualPath != expectedRoute.Path {
					t.Fatalf("expected AppRoutes[%d].Routes[%d].Config.Http.Path to be %s, got %s", i, j, expectedRoute.Path, actualPath)
				}

				// check the certificate if expected
				if expectedRoute.Certificate != nil {
					actualTLS := actualConfig.Http.Tls
					if actualTLS == nil {
						t.Fatalf("expected AppRoutes[%d].Routes[%d].Config.Http.Tls to be set", i, j)
					}
					if !reflect.DeepEqual(actualTLS.Certificate.Chain, expectedRoute.Certificate.Chain) {
						t.Fatalf("expected AppRoutes[%d].Routes[%d].Config.Http.Tls.Certificate.Chain to be %x, got %x", i, j, expectedRoute.Certificate.Chain, actualTLS.Certificate.Chain)
					}
				}
			case "tcp":
				// check we have a TCP config
				actualConfig, ok := actualRoute.Config.(*api.Route_Tcp)
				if !ok {
					t.Fatalf("expected AppRoutes[%d].Routes[%d].Config to be *api.Route_Tcp, got %T", i, j, actualRoute.Config)
				}

				// check the port matches
				if actualPort := actualConfig.Tcp.Port.Port; actualPort != uint32(expectedRoute.Port) {
					t.Fatalf("expected AppRoutes[%d].Routes[%d].Config.Tcp.Port.Port to be %d, got %d", i, j, expectedRoute.Port, actualPort)
				}
			}
		}
	}
}
