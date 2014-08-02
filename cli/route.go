package main

import (
	"bytes"
	"encoding/pem"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"strconv"

	"github.com/flynn/flynn-controller/client"
	"github.com/flynn/strowger/types"
)

var cmdRouteAddHTTP = &Command{
	Run:   runRouteAddHTTP,
	Usage: "route-add-http [-s <service>] [-c <tls-cert>] [-k <tls-key>] <domain>",
	Short: "add a HTTP route",
	Long:  `Add a HTTP route to an app`,
}

var routeHTTPService string
var tlsCertPath string
var tlsKeyPath string
var sticky bool

func init() {
	cmdRouteAddHTTP.Flag.StringVarP(&routeHTTPService, "service", "s", "", "service name to route domain to (defaults to APPNAME-web)")
	cmdRouteAddHTTP.Flag.StringVarP(&tlsCertPath, "tls-cert", "c", "", "path to PEM encoded certificate for TLS, - for stdin")
	cmdRouteAddHTTP.Flag.StringVarP(&tlsKeyPath, "tls-key", "k", "", "path to PEM encoded private key for TLS, - for stdin")
	cmdRouteAddHTTP.Flag.BoolVarP(&sticky, "sticky", "t", false, "enable cookie-based sticky routing")
}

func runRouteAddHTTP(cmd *Command, args []string, client *controller.Client) error {
	var tlsCert []byte
	var tlsKey []byte

	if len(args) != 1 {
		cmd.printUsage(true)
	}

	if routeHTTPService == "" {
		routeHTTPService = mustApp() + "-web"
	}

	if tlsCertPath != "" && tlsKeyPath != "" {
		var stdin []byte
		var err error

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
	} else if tlsCertPath != "" || tlsKeyPath != "" {
		return errors.New("Both the TLS certificate AND private key need to be specified")
	}

	hr := &strowger.HTTPRoute{
		Service: routeHTTPService,
		Domain:  args[0],
		TLSCert: string(tlsCert),
		TLSKey:  string(tlsKey),
		Sticky:  sticky,
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

var cmdRouteRemove = &Command{
	Run:   runRouteRemove,
	Usage: "route-remove <id>",
	Short: "remove a route",
	Long:  "Command route-remove removes a route from the Flynn controller.",
}

func runRouteRemove(cmd *Command, args []string, client *controller.Client) error {
	if len(args) != 1 {
		cmd.printUsage(true)
	}
	routeID := args[0]

	if err := client.DeleteRoute(mustApp(), routeID); err != nil {
		return err
	}
	fmt.Printf("Route %s removed.\n", routeID)
	return nil
}
