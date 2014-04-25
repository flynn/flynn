package main

import (
	"fmt"
	"strconv"

	"github.com/flynn/flynn-controller/client"
	"github.com/flynn/strowger/types"
)

var cmdRouteAddHTTP = &Command{
	Run:   runRouteAddHTTP,
	Usage: "route-add-http [-s <service>] <domain>",
	Short: "add a HTTP route",
	Long:  `Add a HTTP route to an app"`,
}

var routeHTTPService string

func init() {
	cmdRouteAddHTTP.Flag.StringVarP(&routeHTTPService, "service", "s", "", "service name to route domain to (defaults to APPNAME-web)")
}

func runRouteAddHTTP(cmd *Command, args []string, client *controller.Client) error {
	if len(args) != 1 {
		cmd.printUsage(true)
	}
	hr := &strowger.HTTPRoute{Domain: args[0], Service: routeHTTPService}
	if hr.Service == "" {
		hr.Service = mustApp() + "-web"
	}
	route := hr.ToRoute()
	if err := client.CreateRoute(mustApp(), route); err != nil {
		return err
	}
	fmt.Println(route.ID)
	return nil
}

var cmdRoutes = &Command{
	Run:   runRoutes,
	Usage: "routes",
	Short: "list routes",
	Long:  `list routes for application"`,
}

func runRoutes(cmd *Command, args []string, client *controller.Client) error {
	if len(args) != 0 {
		cmd.printUsage(true)
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
