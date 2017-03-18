package main

import (
	"bytes"
	"encoding/pem"
	"errors"
	"fmt"
	"io/ioutil"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/flynn/flynn/controller/client"
	"github.com/flynn/flynn/router/types"
	"github.com/flynn/go-docopt"
)

func init() {
	register("route", runRoute, `
usage: flynn route
       flynn route add http [-s <service>] [-p <port>] [-c <tls-cert> -k <tls-key>] [--sticky] [--leader] [--no-leader] [--no-drain-backends] <domain>
       flynn route add tcp [-s <service>] [-p <port>] [--leader] [--no-drain-backends]
       flynn route update <id> [-s <service>] [-c <tls-cert> -k <tls-key>] [--sticky] [--no-sticky] [--leader] [--no-leader]
       flynn route remove <id>

Manage routes for application.

Options:
	-s, --service=<service>    service name to route domain to (defaults to APPNAME-web)
	-c, --tls-cert=<tls-cert>  path to PEM encoded certificate for TLS, - for stdin (http only)
	-k, --tls-key=<tls-key>    path to PEM encoded private key for TLS, - for stdin (http only)
	--sticky                   enable cookie-based sticky routing (http only)
	--no-sticky                disable cookie-based sticky routing (update http only)
	--leader                   enable leader-only routing mode
	--no-leader                disable leader-only routing mode (update only)
	-p, --port=<port>          port to accept traffic on
	--no-drain-backends        don't wait for in-flight requests to complete before stopping backends

Commands:
	With no arguments, shows a list of routes.

	add     adds a route to an app
	remove  removes a route

Examples:

	$ flynn route add http example.com

	$ flynn route add http example.com/path/

	$ flynn route add tcp

	$ flynn route add tcp --leader
`)
}

func runRoute(args *docopt.Args, client controller.Client) error {
	if args.Bool["add"] {
		switch {
		case args.Bool["http"]:
			return runRouteAddHTTP(args, client)
		case args.Bool["tcp"]:
			return runRouteAddTCP(args, client)
		default:
			return fmt.Errorf("Route type %s not supported.", args.String["-t"])
		}
	} else if args.Bool["update"] {
		typ := strings.Split(args.String["<id>"], "/")[0]
		switch typ {
		case "http":
			return runRouteUpdateHTTP(args, client)
		case "tcp":
			return runRouteUpdateTCP(args, client)
		default:
			return fmt.Errorf("Route type %s not supported.", typ)
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

	var route, port, protocol, service, sticky, path string
	listRec(w, "ROUTE", "SERVICE", "ID", "STICKY", "LEADER", "PATH")
	for _, k := range routes {
		port = strconv.Itoa(int(k.Port))
		switch k.Type {
		case "tcp":
			route = port
			protocol = "tcp"
			service = k.TCPRoute().Service
		case "http":
			route = k.HTTPRoute().Domain
			if port != "0" {
				route = k.HTTPRoute().Domain + ":" + port
			}
			service = k.TCPRoute().Service
			httpRoute := k.HTTPRoute()
			if httpRoute.Certificate == nil && httpRoute.LegacyTLSCert == "" {
				protocol = "http"
			} else {
				protocol = "https"
			}
			sticky = fmt.Sprintf("%t", k.Sticky)
			path = k.HTTPRoute().Path
		}
		listRec(w, protocol+":"+route, service, k.FormattedID(), sticky, k.Leader, path)
	}
	return nil
}

func runRouteAddTCP(args *docopt.Args, client controller.Client) error {
	service := args.String["--service"]
	if service == "" {
		service = mustApp() + "-web"
	}

	port := 0
	if args.String["--port"] != "" {
		p, err := strconv.Atoi(args.String["--port"])
		if err != nil {
			return err
		}
		port = p
	}

	hr := &router.TCPRoute{
		Service:       service,
		Port:          port,
		Leader:        args.Bool["--leader"],
		DrainBackends: !args.Bool["--no-drain-backends"],
	}

	r := hr.ToRoute()
	if err := client.CreateRoute(mustApp(), r); err != nil {
		return err
	}
	hr = r.TCPRoute()
	fmt.Printf("%s listening on port %d\n", hr.FormattedID(), hr.Port)
	return nil
}

func runRouteAddHTTP(args *docopt.Args, client controller.Client) error {
	service := args.String["--service"]
	if service == "" {
		service = mustApp() + "-web"
	}

	tlsCert, tlsKey, err := parseTLSCert(args)
	if err != nil {
		return err
	}

	port := 0
	if args.String["--port"] != "" {
		p, err := strconv.Atoi(args.String["--port"])
		if err != nil {
			return err
		}
		port = p
	}

	u, err := url.Parse("http://" + args.String["<domain>"])
	if err != nil {
		return fmt.Errorf("Failed to parse %s as URL", args.String["<domain>"])
	}

	hr := &router.HTTPRoute{
		Service:       service,
		Domain:        u.Host,
		Port:          port,
		LegacyTLSCert: tlsCert,
		LegacyTLSKey:  tlsKey,
		Sticky:        args.Bool["--sticky"],
		Leader:        args.Bool["--leader"],
		Path:          u.Path,
		DrainBackends: !args.Bool["--no-drain-backends"],
	}
	route := hr.ToRoute()
	if err := client.CreateRoute(mustApp(), route); err != nil {
		return err
	}
	fmt.Println(route.FormattedID())
	return nil
}

func runRouteUpdateTCP(args *docopt.Args, client controller.Client) error {
	id := args.String["<id>"]
	appName := mustApp()

	route, err := client.GetRoute(appName, id)
	if err != nil {
		return err
	}

	service := args.String["--service"]
	if service == "" {
		return errors.New("No service name given")
	}
	route.Service = service

	if args.Bool["--leader"] {
		route.Leader = true
	} else if args.Bool["--no-leader"] {
		route.Leader = false
	}

	if err := client.UpdateRoute(appName, id, route); err != nil {
		return err
	}
	hr := route.TCPRoute()
	fmt.Printf("%s listening on port %d\n", hr.FormattedID(), hr.Port)
	return nil
}

func runRouteUpdateHTTP(args *docopt.Args, client controller.Client) error {
	id := args.String["<id>"]
	appName := mustApp()

	route, err := client.GetRoute(appName, id)
	if err != nil {
		return err
	}

	if service := args.String["--service"]; service != "" {
		route.Service = service
	}

	route.Certificate = nil
	route.LegacyTLSCert, route.LegacyTLSKey, err = parseTLSCert(args)
	if err != nil {
		return err
	}

	if args.Bool["--sticky"] {
		route.Sticky = true
	} else if args.Bool["--no-sticky"] {
		route.Sticky = false
	}

	if args.Bool["--leader"] {
		route.Leader = true
	} else if args.Bool["--no-leader"] {
		route.Leader = false
	}

	if err := client.UpdateRoute(appName, id, route); err != nil {
		return err
	}
	fmt.Printf("updated %s\n", route.FormattedID())
	return nil
}

func parseTLSCert(args *docopt.Args) (string, string, error) {
	tlsCertPath := args.String["--tls-cert"]
	tlsKeyPath := args.String["--tls-key"]
	var tlsCert []byte
	var tlsKey []byte
	if tlsCertPath != "" && tlsKeyPath != "" {
		var stdin []byte

		if tlsCertPath == "-" || tlsKeyPath == "-" {
			var err error
			stdin, err = ioutil.ReadAll(os.Stdin)
			if err != nil {
				return "", "", fmt.Errorf("Failed to read from stdin: %s", err)
			}
		}

		var err error
		tlsCert, err = readPEM("CERTIFICATE", tlsCertPath, stdin)
		if err != nil {
			return "", "", fmt.Errorf("Failed to read TLS cert: %s", err)
		}
		tlsKey, err = readPEM("PRIVATE KEY", tlsKeyPath, stdin)
		if err != nil {
			return "", "", fmt.Errorf("Failed to read TLS key: %s", err)
		}
	} else if tlsCertPath != "" || tlsKeyPath != "" {
		return "", "", errors.New("Both the TLS certificate AND private key need to be specified")
	}
	return string(tlsCert), string(tlsKey), nil
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

func runRouteRemove(args *docopt.Args, client controller.Client) error {
	routeID := args.String["<id>"]

	if err := client.DeleteRoute(mustApp(), routeID); err != nil {
		return err
	}
	fmt.Printf("Route %s removed.\n", routeID)
	return nil
}
