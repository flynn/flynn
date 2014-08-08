package main

import (
	"bytes"
	"encoding/pem"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"strconv"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-docopt"
	"github.com/flynn/flynn/controller/client"
	"github.com/flynn/flynn/router/types"
)

func runRoute(argv []string, client *controller.Client) error {
	usage := `usage: flynn route
       flynn route add [-t <type>] [-s <service>] [-c <tls-cert> -k <tls-key>] [--sticky] <domain>
       flynn route remove <id>

Manage routes for application.

Options:
   -t <type>                  route's type (Currently only http supported) [default: http]
   -s, --service <service>    service name to route domain to (defaults to APPNAME-web)
   -c, --tls-cert <tls-cert>  path to PEM encoded certificate for TLS, - for stdin
   -k, --tls-key <tls-key>    path to PEM encoded private key for TLS, - for stdin
   --sticky                   enable cookie-based sticky routing
Commands:
   With no arguments, shows a list of routes.

   add     adds a route to an app
   remove  removes a route
`
	args, _ := docopt.Parse(usage, argv, true, "", false)

	if args.Bool["add"] {
		if args.String["-t"] == "http" {
			return runRouteAddHTTP(args, client)
		} else {
			return fmt.Errorf("Route type %s not supported.", args.String["-t"])
		}
	} else if args.Bool["remove"] {
		return runRouteRemove(args, client)
	}

	routes, err := client.RouteList(mustApp())
	if err != nil {
		return err
	}

	w := tabWriter()
	defer w.Flush()

	var route, protocol, service string
	listRec(w, "ROUTE", "SERVICE", "ID")
	for _, k := range routes {
		switch k.Type {
		case "tcp":
			protocol = "tcp"
			route = strconv.Itoa(k.TCPRoute().Port)
			service = k.TCPRoute().Service
		case "http":
			route = k.HTTPRoute().Domain
			service = k.TCPRoute().Service
			if k.HTTPRoute().TLSCert == "" {
				protocol = "http"
			} else {
				protocol = "https"
			}
		}
		listRec(w, protocol+":"+route, service, k.ID)
	}
	return nil
}

func runRouteAddHTTP(args *docopt.Args, client *controller.Client) error {
	var tlsCert []byte
	var tlsKey []byte

	var routeHTTPService string
	if args.String["--service"] == "" {
		routeHTTPService = mustApp() + "-web"
	} else {
		routeHTTPService = args.String["--service"]
	}

	if args.String["tls-cert"] != "" && args.String["tls-key"] != "" {
		var stdin []byte
		var err error

		tlsCertPath := args.String["tls-cert"]
		tlsKeyPath := args.String["tls-key"]

		if tlsCertPath == "-" || tlsKeyPath == "-" {
			stdin, err = ioutil.ReadAll(os.Stdin)
			if err != nil {
				return fmt.Errorf("Failed to read from stdin: %s", err)
			}
		}

		tlsCert, err = readPEM("CERTIFICATE", tlsCertPath, stdin)
		if err != nil {
			return errors.New("Failed to read TLS Cert")
		}
		tlsKey, err = readPEM("PRIVATE KEY", tlsKeyPath, stdin)
		if err != nil {
			return errors.New("Failed to read TLS Key")
		}
	} else if args.String["tls-cert"] != "" || args.String["tls-key"] != "" {
		return errors.New("Both the TLS certificate AND private key need to be specified")
	}

	hr := &strowger.HTTPRoute{
		Service: routeHTTPService,
		Domain:  args.String["<domain>"],
		TLSCert: string(tlsCert),
		TLSKey:  string(tlsKey),
		Sticky:  args.Bool["sticky"],
	}
	route := hr.ToRoute()
	if err := client.CreateRoute(mustApp(), route); err != nil {
		return err
	}
	fmt.Println(route.ID)
	return nil
}

func readPEM(typ string, path string, stdin []byte) ([]byte, error) {
	if path == "-" {
		var buf bytes.Buffer
		var block *pem.Block
		for {
			block, stdin = pem.Decode(stdin)
			if block == nil {
				break
			}
			if block.Type == typ {
				pem.Encode(&buf, block)
			}
		}
		if buf.Len() > 0 {
			return buf.Bytes(), nil
		}
		return nil, errors.New("No PEM blocks found in stdin")
	}
	return ioutil.ReadFile(path)
}

func runRouteRemove(args *docopt.Args, client *controller.Client) error {
	routeID := args.String["<id>"]

	if err := client.DeleteRoute(mustApp(), routeID); err != nil {
		return err
	}
	fmt.Printf("Route %s removed.\n", routeID)
	return nil
}
