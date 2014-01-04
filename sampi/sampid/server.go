package main

import (
	"flag"
	"net"
	"net/http"
	"os"

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
	if err = d.Register("flynn-sampi", *listenAddr); err != nil {
		g.Log(grohl.Data{"at": "discover_registration", "status": "error", "err": err})
		os.Exit(1)
	}

	http.Serve(l, nil)
}
