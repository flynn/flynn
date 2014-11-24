package main

import (
	"encoding/base64"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path"
	"strings"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/coreos/go-etcd/etcd"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/kavu/go_reuseport"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/shutdown"
	"github.com/flynn/flynn/router/types"
)

type Listener interface {
	Start() error
	Close() error
	AddRoute(*router.Route) error
	SetRoute(*router.Route) error
	RemoveRoute(id string) error
	Watcher
	DataStoreReader
}

type Router struct {
	HTTP Listener
	TCP  Listener
}

func (s *Router) ListenAndServe(quit <-chan struct{}) error {
	if err := s.HTTP.Start(); err != nil {
		return err
	}
	if err := s.TCP.Start(); err != nil {
		return err
	}
	<-quit
	// TODO: unregister from service discovery
	s.HTTP.Close()
	// TODO: wait for client connections to finish
	return nil
}

func main() {
	apiPort := os.Getenv("PORT")
	if apiPort == "" {
		apiPort = "5000"
	}
	var cookieKey *[32]byte
	if key := os.Getenv("COOKIE_KEY"); key != "" {
		res, err := base64.StdEncoding.DecodeString(key)
		if err != nil {
			log.Fatal("error decoding COOKIE_KEY:", err)
		}
		var k [32]byte
		copy(k[:], res)
		cookieKey = &k
	}

	httpAddr := flag.String("httpaddr", ":8080", "http listen address")
	httpsAddr := flag.String("httpsaddr", ":4433", "https listen address")
	tcpIP := flag.String("tcpip", "", "tcp router listen ip")
	tcpRangeStart := flag.Int("tcp-range-start", 3000, "tcp port range start")
	tcpRangeEnd := flag.Int("tcp-range-end", 3500, "tcp port range end")
	apiAddr := flag.String("apiaddr", ":"+apiPort, "api listen address")
	flag.Parse()

	// Will use DISCOVERD environment variable
	d, err := discoverd.NewClient()
	if err != nil {
		log.Fatal(err)
	}
	services := map[string]string{
		"router-api":  *apiAddr,
		"router-http": *httpAddr,
	}
	for service, addr := range services {
		if err := d.Register(service, addr); err != nil {
			log.Fatal(err)
		}
	}

	shutdown.BeforeExit(func() {
		for service, addr := range services {
			discoverd.Unregister(service, addr)
		}
	})

	// Read etcd addresses from ETCD
	etcdAddrs := strings.Split(os.Getenv("ETCD"), ",")
	if len(etcdAddrs) == 1 && etcdAddrs[0] == "" {
		if externalIP := os.Getenv("EXTERNAL_IP"); externalIP != "" {
			etcdAddrs = []string{fmt.Sprintf("http://%s:2379", externalIP)}
		} else {
			etcdAddrs = nil
		}
	}
	etcdc := etcd.NewClient(etcdAddrs)

	prefix := os.Getenv("ETCD_PREFIX")
	if prefix == "" {
		prefix = "/router"
	}
	var r Router
	r.TCP = NewTCPListener(*tcpIP, *tcpRangeStart, *tcpRangeEnd, NewEtcdDataStore(etcdc, path.Join(prefix, "tcp/")), d)
	r.HTTP = NewHTTPListener(*httpAddr, *httpsAddr, cookieKey, NewEtcdDataStore(etcdc, path.Join(prefix, "http/")), d)

	go func() { log.Fatal(r.ListenAndServe(nil)) }()
	listener, err := reuseport.NewReusablePortListener("tcp4", *apiAddr)
	if err != nil {
		log.Fatal(err)
	}
	log.Fatal(http.Serve(listener, apiHandler(&r)))
}
