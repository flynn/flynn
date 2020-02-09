package main

import (
	"bytes"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/flynn/flynn/controller/api"
	controller "github.com/flynn/flynn/controller/client"
	"github.com/flynn/flynn/controller/data"
	"github.com/flynn/flynn/pkg/routeconfig"
	router "github.com/flynn/flynn/router/types"
	"github.com/flynn/go-docopt"
	"github.com/olekukonko/tablewriter"
)

func init() {
	register("route", runRoute, `
usage: flynn route
       flynn route config generate [-f <file>] [<apps>...]
       flynn route config apply [--force] <file>
       flynn route add http [-s <service>] [-p <port>] [-c <tls-cert> -k <tls-key>] [--sticky] [--leader] [--no-leader] [--no-drain-backends] [--disable-keep-alives] <domain>
       flynn route add tcp [-s <service>] [-p <port>] [--leader] [--no-drain-backends]
       flynn route update <id> [-s <service>] [-c <tls-cert> -k <tls-key>] [--sticky] [--no-sticky] [--leader] [--no-leader] [--disable-keep-alives] [--enable-keep-alives]
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
	--disable-keep-alives      disable keep-alives between the router and backends for the given route
	--enable-keep-alives       enable keep-alives between the router and backends for the given route (default for new routes)
	-f, --file=<file>          name of file to write generated config to (defaults to stdout)
	--force                    forcibly apply route changes without user confirmation

Commands:
	With no arguments, shows a list of routes.

	config  generates or applies route config
	add     adds a route to an app
	update  updates a route
	remove  removes a route

Examples:

	$ flynn route config generate -f routes.cfg app1 app2 app3

	$ flynn route config apply routes.cfg

	$ flynn route add http example.com

	$ flynn route add http example.com/path/

	$ flynn route add tcp

	$ flynn route add tcp --leader
`)
}

func runRoute(args *docopt.Args, client controller.Client) error {
	if args.Bool["config"] {
		switch {
		case args.Bool["generate"]:
			return runRouteConfigGenerate(args, client)
		case args.Bool["apply"]:
			return runRouteConfigApply(args, client)
		default:
			return errors.New("unknown route config command")
		}
	} else if args.Bool["add"] {
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

	routes, err := client.AppRouteList(mustApp())
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

func runRouteConfigGenerate(args *docopt.Args, client controller.Client) error {
	// list routes for the given apps or the current app
	apps := args.All["<apps>"].([]string)
	if len(apps) == 0 {
		app, err := app()
		if err != nil {
			return errors.New("no app found, run from a repo with a flynn remote, specify one with -a or pass a list of apps on the command line")
		}
		apps = []string{app}
	}
	req := api.ListAppRoutesRequest{Apps: apps}
	var res api.ListAppRoutesResponse
	if err := client.Invoke("flynn.api.v1.Router/ListAppRoutes", &req, &res); err != nil {
		return fmt.Errorf("error getting routes: %s", err)
	}

	// generate the route config
	config, err := routeconfig.Generate(apps, res.AppRoutes)
	if err != nil {
		return fmt.Errorf("error generating config: %s", err)
	}

	// open the output file
	var out io.Writer = os.Stdout
	if file := args.String["--file"]; file != "" {
		f, err := os.Create(file)
		if err != nil {
			return err
		}
		defer f.Close()
		out = f
	}

	// write the config
	if _, err := out.Write(config); err != nil {
		return fmt.Errorf("error writing config: %s", err)
	}

	return nil
}

func runRouteConfigApply(args *docopt.Args, client controller.Client) error {
	// load the app routes from the route config
	var in io.Reader
	if args.String["<file>"] == "-" {
		in = os.Stdin
	} else {
		f, err := os.Open(args.String["<file>"])
		if err != nil {
			return err
		}
		defer f.Close()
		in = f
	}
	appRoutes, err := routeconfig.Load(in)
	if err != nil {
		return err
	}

	// perform the request
	req := api.SetRoutesRequest{
		AppRoutes: appRoutes,
		DryRun:    !args.Bool["--force"],
	}
	var res api.SetRoutesResponse
	if err := client.Invoke("flynn.api.v1.Router/SetRoutes", &req, &res); err != nil {
		return fmt.Errorf("error applying config: %s", err)
	}

	// if performing a dry run, confirm the changes with the user then
	// perform the request again
	if len(res.RouteChanges) > 0 && res.DryRun {
		if !confirmRouteChanges(res.RouteChanges) {
			return errors.New("Cancelling route change at user request.")
		}
		req.DryRun = false
		req.ExpectedState = res.AppliedToState
		if err := client.Invoke("flynn.api.v1.Router/SetRoutes", &req, &res); err != nil {
			return fmt.Errorf("error applying config: %s", err)
		}
		fmt.Println("The confirmed route changes were made successfully.")
		return nil
	}

	// report the changes
	switch len(res.RouteChanges) {
	case 0:
		fmt.Println("No route changes were made as all routes are up-to-date.")
	case 1:
		fmt.Println("The following route change was made:")
		printRouteChanges(res.RouteChanges)
	default:
		fmt.Println("The following", len(res.RouteChanges), "route changes were made:")
		printRouteChanges(res.RouteChanges)
	}

	return nil
}

func confirmRouteChanges(changes []*api.RouteChange) bool {
	fmt.Println()
	if len(changes) == 1 {
		fmt.Println("!!! You are about to make the following route change: !!!")
	} else {
		fmt.Printf("!!! You are about to make the following %d route changes: !!!\n", len(changes))
	}
	fmt.Println()
	printRouteChanges(changes)
	fmt.Println()
	return promptYesNo("Are you sure you want to proceed?")
}

func printRouteChanges(changes []*api.RouteChange) {
	table := tablewriter.NewWriter(os.Stdout)
	table.SetRowLine(true)
	table.SetAutoWrapText(false)
	table.SetHeader([]string{"ACTION", "ROUTE", "BEFORE", "AFTER"})
	for _, change := range changes {
		var (
			action string
			route  *router.Route
		)
		switch change.Action {
		case api.RouteChange_ACTION_CREATE:
			action = "CREATE"
			route = data.ToRouterRoute("", change.After)
		case api.RouteChange_ACTION_UPDATE:
			action = "UPDATE"
			route = data.ToRouterRoute("", change.After)
		case api.RouteChange_ACTION_DELETE:
			action = "DELETE"
			route = data.ToRouterRoute("", change.Before)
		}

		var routeDesc string
		switch route.Type {
		case "http":
			routeDesc = fmt.Sprintf("http:%s%s", route.Domain, route.Path)
		case "tcp":
			routeDesc = fmt.Sprintf("tcp:%d", route.Port)
		}

		table.Append([]string{
			action,
			routeDesc,
			formatRouteConfig(change.Before),
			formatRouteConfig(change.After),
		})
	}
	table.Render()
}

func formatRouteConfig(route *api.Route) string {
	if route == nil {
		return "-"
	}

	r := data.ToRouterRoute("", route)
	var lines []string

	service := "service: " + r.Service
	if r.Leader {
		service += " (leader)"
	}
	if !r.DrainBackends {
		service += " (no-drain-backends)"
	}
	lines = append(lines, service)

	if r.Sticky {
		lines = append(lines, "sticky sessions: true")
	}

	if r.DisableKeepAlives {
		lines = append(lines, "disable keep alives: true")
	}

	return strings.Join(lines, "\n")
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
		Service:           service,
		Domain:            u.Host,
		Port:              port,
		LegacyTLSCert:     tlsCert,
		LegacyTLSKey:      tlsKey,
		Sticky:            args.Bool["--sticky"],
		Leader:            args.Bool["--leader"],
		Path:              u.Path,
		DrainBackends:     !args.Bool["--no-drain-backends"],
		DisableKeepAlives: args.Bool["--disable-keep-alives"],
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

	if args.Bool["--disable-keep-alives"] {
		route.DisableKeepAlives = true
	} else if args.Bool["--enable-keep-alives"] {
		route.DisableKeepAlives = false
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
