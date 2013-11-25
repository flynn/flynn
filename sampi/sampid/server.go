package main

import (
	"flag"
	"log"
	"net"
	"net/http"
	"strings"

	"github.com/flynn/go-discover/discover"
	rpc "github.com/flynn/rpcplus/comborpc"
)

var listenAddr = flag.String("listen", ":1112", "listen address")

func main() {
	flag.Parse()
	rpc.HandleHTTP()
	l, err := net.Listen("tcp", *listenAddr)
	if err != nil {
		log.Fatal(err)
	}

	d, err := discover.NewClient()
	if err != nil {
		log.Fatal(err)
	}
	if hostPort := strings.SplitN(*listenAddr, ":", 2); hostPort[0] != "" {
		err = d.RegisterWithHost("flynn-sampi", hostPort[0], hostPort[1], nil)
	} else {
		err = d.Register("flynn-sampi", hostPort[1], nil)
	}
	if err != nil {
		log.Fatal(err)
	}

	http.Serve(l, nil)
}
