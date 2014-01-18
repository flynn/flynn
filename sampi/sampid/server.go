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

	leader, err := discoverd.RegisterAndStandby("flynn-sampi", *listenAddr, nil)
	if err != nil {
		g.Log(grohl.Data{"at": "discover_registration", "status": "error", "err": err})
		os.Exit(1)
	}

	select {
	case <-leader:
	default:
		g.Log(grohl.Data{"at": "standby"})
		<-leader
	}
	g.Log(grohl.Data{"at": "listening"})
	http.Serve(l, nil)
}
