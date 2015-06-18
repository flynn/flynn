package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/coreos/go-etcd/etcd"
	"github.com/flynn/flynn/discoverd/server"
	"github.com/flynn/flynn/pkg/attempt"
	"github.com/flynn/flynn/pkg/shutdown"
)

func main() {
	defer shutdown.Exit()

	httpAddr := flag.String("http-addr", ":1111", "address to serve HTTP API from")
	dnsAddr := flag.String("dns-addr", ":53", "address to service DNS from")
	resolvers := flag.String("recursors", "8.8.8.8,8.8.4.4", "upstream recursive DNS servers")
	etcdAddrs := flag.String("etcd", "http://127.0.0.1:2379", "etcd servers (comma separated)")
	notify := flag.String("notify", "", "url to send webhook to after starting listener")
	flag.Parse()

	etcdClient := etcd.NewClient(strings.Split(*etcdAddrs, ","))

	// Check to make sure that etcd is online and accepting connections
	// etcd takes a while to come online, so we attempt a GET multiple times
	err := attempt.Strategy{
		Min:   5,
		Total: 10 * time.Minute,
		Delay: 200 * time.Millisecond,
	}.Run(func() (err error) {
		_, err = etcdClient.Get("/", false, false)
		if e, ok := err.(*etcd.EtcdError); ok && e.ErrorCode == 100 {
			// Valid 404 from etcd (> v2.0)
			err = nil
		}
		return
	})
	if err != nil {
		log.Fatalf("Failed to connect to etcd at %v: %q", etcdAddrs, err)
	}

	state := server.NewState()
	backend := server.NewEtcdBackend(etcdClient, "/discoverd", state)
	if err := backend.StartSync(); err != nil {
		log.Fatalf("Failed to perform initial etcd sync: %s", err)
	}

	dns := server.DNSServer{
		UDPAddr: *dnsAddr,
		TCPAddr: *dnsAddr,
		Store:   state,
	}
	if *resolvers != "" {
		dns.Recursors = strings.Split(*resolvers, ",")
	}
	if err := dns.ListenAndServe(); err != nil {
		log.Fatalf("Failed to start DNS server: %s", err)
	}

	l, err := net.Listen("tcp4", *httpAddr)
	if err != nil {
		log.Fatalf("Failed to start HTTP listener: %s", err)
	}
	log.Printf("discoverd listening for HTTP on %s and DNS on %s", *httpAddr, *dnsAddr)

	if *notify != "" {
		addr := l.Addr().String()
		host, port, _ := net.SplitHostPort(addr)
		if host == "0.0.0.0" {
			// try to get real address from dns addr
			if dnsHost, _, _ := net.SplitHostPort(*dnsAddr); dnsHost != "" {
				addr = net.JoinHostPort(dnsHost, port)
			}
		}
		data := struct {
			URL string `json:"url"`
		}{fmt.Sprintf("http://%s", addr)}
		payload, _ := json.Marshal(data)
		res, err := http.Post(*notify, "application/json", bytes.NewReader(payload))
		if err != nil {
			log.Printf("failed to notify: %s", err)
		} else {
			res.Body.Close()
		}
	}

	http.Serve(l, server.NewHTTPHandler(server.NewBasicDatastore(state, backend)))
}
