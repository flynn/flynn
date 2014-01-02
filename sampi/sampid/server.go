package main

import (
	"flag"
	"net"
	"net/http"
	"os"
	"strings"

	"github.com/flynn/go-discoverd"
	rpc "github.com/flynn/rpcplus/comborpc"
	"github.com/technoweenie/grohl"
)

var listenAddr = flag.String("listen", ":1112", "listen address")

func main() {
	flag.Parse()
	g := grohl.NewContext(grohl.Data{"app": "sampi"})
	g.Log(grohl.Data{"at": "start"})
	rpc.HandleHTTP()
	l, err := net.Listen("tcp", *listenAddr)
	if err != nil {
		g.Log(grohl.Data{"at": "listen", "status": "error", "err": err})
		os.Exit(1)
	}

	d, err := discoverd.NewClient()
	if err != nil {
		g.Log(grohl.Data{"at": "discover_connect", "status": "error", "err": err})
		os.Exit(1)
	}
	if hostPort := strings.SplitN(*listenAddr, ":", 2); hostPort[0] != "" {
		err = d.RegisterWithHost("flynn-sampi", hostPort[0], hostPort[1], nil)
	} else {
		err = d.Register("flynn-sampi", hostPort[1], nil)
	}
	if err != nil {
		g.Log(grohl.Data{"at": "discover_registration", "status": "error", "err": err})
		os.Exit(1)
	}

	http.Serve(l, nil)
}
