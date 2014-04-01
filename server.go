package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/flynn/go-discoverd"
	"github.com/flynn/go-etcd/etcd"
	rpc "github.com/flynn/rpcplus/comborpc"
)

type Router struct {
	*HTTPListener
}

func (s *Router) ListenAndServe(quit <-chan struct{}) error {
	if err := s.HTTPListener.Start(); err != nil {
		return err
	}
	<-quit
	// TODO: unregister from service discovery
	s.HTTPListener.Close()
	// TODO: wait for client connections to finish
	return nil
}

func main() {
	rpcAddr := flag.String("rpcaddr", ":1115", "rpc listen address")
	httpAddr := flag.String("httpaddr", ":8080", "http listen address")
	httpsAddr := flag.String("httpsaddr", ":4433", "https listen address")
	flag.Parse()

	// Will use DISCOVERD environment variable
	d, err := discoverd.NewClient()
	if err != nil {
		log.Fatal(err)
	}

	// Read etcd address from ETCD
	etcdAddr := strings.Split(os.Getenv("ETCD"), ",")
	if len(etcdAddr) == 1 && etcdAddr[0] == "" {
		etcdAddr = nil
	}

	var r Router
	r.HTTPListener = NewHTTPListener(*httpAddr, *httpsAddr, etcd.NewClient(etcdAddr), d)
	rpc.RegisterName("Router", &RPCHandler{r})
	rpc.HandleHTTP()
	go http.ListenAndServe(*rpcAddr, nil)

	if err = d.Register("flynn-strowger-rpc", *rpcAddr); err != nil {
		log.Fatal(err)
	}

	r.ListenAndServe(nil)
}
