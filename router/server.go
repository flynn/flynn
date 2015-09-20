package main

import (
	"crypto/tls"
	"encoding/base64"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/vanillahsu/go_reuseport"
	"github.com/flynn/flynn/Godeps/_workspace/src/gopkg.in/inconshreveable/log15.v2"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/postgres"
	"github.com/flynn/flynn/pkg/shutdown"
	"github.com/flynn/flynn/router/types"
)

var logger = log15.New("app", "router")

type Listener interface {
	Start() error
	Close() error
	AddRoute(*router.Route) error
	UpdateRoute(*router.Route) error
	RemoveRoute(id string) error
	Watcher
	DataStoreReader
}

type Router struct {
	HTTP Listener
	TCP  Listener
}

func (s *Router) Start() error {
	log := logger.New("fn", "Start")
	log.Info("starting HTTP listener")
	if err := s.HTTP.Start(); err != nil {
		log.Error("error starting HTTP listener", "err", err)
		return err
	}
	log.Info("starting TCP listener")
	if err := s.TCP.Start(); err != nil {
		log.Error("error starting TCP listener", "err", err)
		return err
	}
	return nil
}

func (s *Router) Close() {
	s.HTTP.Close()
	s.TCP.Close()
}

var listenFunc = reuseport.NewReusablePortListener

func main() {
	defer shutdown.Exit()

	var cookieKey *[32]byte
	if key := os.Getenv("COOKIE_KEY"); key != "" {
		res, err := base64.StdEncoding.DecodeString(key)
		if err != nil {
			shutdown.Fatalf("error decoding COOKIE_KEY: %s", err)
		}
		if len(res) != 32 {
			shutdown.Fatalf("decoded %d bytes from COOKIE_KEY, expected 32", len(res))
		}
		var k [32]byte
		copy(k[:], res)
		cookieKey = &k
	}
	if cookieKey == nil {
		shutdown.Fatal("Missing random 32 byte base64-encoded COOKIE_KEY")
	}

	httpPort := flag.String("http-port", "8080", "http listen port")
	httpsPort := flag.String("https-port", "4433", "https listen port")
	tcpIP := flag.String("tcp-ip", os.Getenv("LISTEN_IP"), "tcp router listen ip")
	tcpRangeStart := flag.Int("tcp-range-start", 3000, "tcp port range start")
	tcpRangeEnd := flag.Int("tcp-range-end", 3500, "tcp port range end")
	certFile := flag.String("tls-cert", "", "TLS (SSL) cert file in pem format")
	keyFile := flag.String("tls-key", "", "TLS (SSL) key file in pem format")
	apiPort := flag.String("api-port", "", "api listen port")
	flag.Parse()

	if *apiPort == "" {
		*apiPort = os.Getenv("PORT")
		if *apiPort == "" {
			*apiPort = "5000"
		}
	}

	keypair := tls.Certificate{}
	var err error
	if *certFile != "" {
		if keypair, err = tls.LoadX509KeyPair(*certFile, *keyFile); err != nil {
			shutdown.Fatal(err)
		}
	} else if tlsCert := os.Getenv("TLSCERT"); tlsCert != "" {
		if tlsKey := os.Getenv("TLSKEY"); tlsKey != "" {
			os.Setenv("TLSKEY", fmt.Sprintf("md5^(%s)", md5sum(tlsKey)))
			if keypair, err = tls.X509KeyPair([]byte(tlsCert), []byte(tlsKey)); err != nil {
				shutdown.Fatal(err)
			}
		}
	}

	log := logger.New("fn", "main")

	log.Info("connecting to postgres")
	db := postgres.Wait(nil, nil)

	log.Info("running DB migrations")
	if err := migrateDB(db); err != nil {
		shutdown.Fatal(err)
	}

	shutdown.BeforeExit(func() { db.Close() })

	httpAddr := net.JoinHostPort(os.Getenv("LISTEN_IP"), *httpPort)
	httpsAddr := net.JoinHostPort(os.Getenv("LISTEN_IP"), *httpsPort)
	r := Router{
		TCP: &TCPListener{
			IP:        *tcpIP,
			startPort: *tcpRangeStart,
			endPort:   *tcpRangeEnd,
			ds:        NewPostgresDataStore("tcp", db.ConnPool),
			discoverd: discoverd.DefaultClient,
		},
		HTTP: &HTTPListener{
			Addr:      httpAddr,
			TLSAddr:   httpsAddr,
			cookieKey: cookieKey,
			keypair:   keypair,
			ds:        NewPostgresDataStore("http", db.ConnPool),
			discoverd: discoverd.DefaultClient,
		},
	}

	if err := r.Start(); err != nil {
		shutdown.Fatal(err)
	}
	shutdown.BeforeExit(r.Close)

	apiAddr := net.JoinHostPort(os.Getenv("LISTEN_IP"), *apiPort)
	log.Info("starting API listener")
	listener, err := listenFunc("tcp4", apiAddr)
	if err != nil {
		log.Error("error starting API listener", "err", err)
		shutdown.Fatal(listenErr{apiAddr, err})
	}

	services := map[string]string{
		"router-api":  apiAddr,
		"router-http": httpAddr,
	}
	for service, addr := range services {
		log.Info("registering service", "name", service, "addr", addr)
		hb, err := discoverd.AddServiceAndRegister(service, addr)
		if err != nil {
			log.Error("error registering service", "name", service, "addr", addr, "err", err)
			shutdown.Fatal(err)
		}
		shutdown.BeforeExit(func() { hb.Close() })
	}

	log.Info("serving API requests")
	shutdown.Fatal(http.Serve(listener, apiHandler(&r)))
}

type listenErr struct {
	Addr string
	Err  error
}

func (e listenErr) Error() string {
	return fmt.Sprintf("error binding to port (check if another service is listening on %s): %s", e.Addr, e.Err)
}
