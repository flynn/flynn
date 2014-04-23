package main

import (
	"fmt"

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
