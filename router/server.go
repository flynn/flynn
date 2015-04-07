package main

import (
	"crypto/tls"
	"encoding/base64"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strconv"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/jackc/pgx"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/kavu/go_reuseport"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/postgres"
	"github.com/flynn/flynn/pkg/shutdown"
	"github.com/flynn/flynn/router/types"
)

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
	if err := s.HTTP.Start(); err != nil {
		return err
	}
	if err := s.TCP.Start(); err != nil {
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

	apiPort := os.Getenv("PORT")
	if apiPort == "" {
		apiPort = "5000"
	}
	var cookieKey *[32]byte
	if key := os.Getenv("COOKIE_KEY"); key != "" {
		res, err := base64.StdEncoding.DecodeString(key)
		if err != nil {
			shutdown.Fatal("error decoding COOKIE_KEY:", err)
		}
		if len(res) != 32 {
			shutdown.Fatal(fmt.Sprintf("decoded %d bytes from COOKIE_KEY, expected 32", len(res)))
		}
		var k [32]byte
		copy(k[:], res)
		cookieKey = &k
	}
	if cookieKey == nil {
		shutdown.Fatal("Missing random 32 byte base64-encoded COOKIE_KEY")
	}

	httpAddr := flag.String("httpaddr", ":8080", "http listen address")
	httpsAddr := flag.String("httpsaddr", ":4433", "https listen address")
	tcpIP := flag.String("tcpip", "", "tcp router listen ip")
	tcpRangeStart := flag.Int("tcp-range-start", 3000, "tcp port range start")
	tcpRangeEnd := flag.Int("tcp-range-end", 3500, "tcp port range end")
	certFile := flag.String("tlscert", "", "TLS (SSL) cert file in pem format")
	keyFile := flag.String("tlskey", "", "TLS (SSL) key file in pem format")
	apiAddr := flag.String("apiaddr", ":"+apiPort, "api listen address")
	flag.Parse()

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

	db, err := postgres.Open("", "")
	if err != nil {
		shutdown.Fatal(err)
	}
	if err := migrateDB(db.DB); err != nil {
		shutdown.Fatal(err)
	}

	var pgport int
	if port := os.Getenv("PGPORT"); port != "" {
		var err error
		if pgport, err = strconv.Atoi(port); err != nil {
			shutdown.Fatal(err)
		}
	}

	pgxpool, err := pgx.NewConnPool(pgx.ConnPoolConfig{
		ConnConfig: pgx.ConnConfig{
			Host:     os.Getenv("PGHOST"),
			Port:     uint16(pgport),
			Database: os.Getenv("PGDATABASE"),
			User:     os.Getenv("PGUSER"),
			Password: os.Getenv("PGPASSWORD"),
		},
	})
	if err != nil {
		shutdown.Fatal(err)
	}
	shutdown.BeforeExit(func() { pgxpool.Close() })

	r := Router{
		TCP: &TCPListener{
			IP:        *tcpIP,
			startPort: *tcpRangeStart,
			endPort:   *tcpRangeEnd,
			ds:        NewPostgresDataStore("tcp", pgxpool),
			discoverd: discoverd.DefaultClient,
		},
		HTTP: &HTTPListener{
			Addr:      *httpAddr,
			TLSAddr:   *httpsAddr,
			cookieKey: cookieKey,
			keypair:   keypair,
			ds:        NewPostgresDataStore("http", pgxpool),
			discoverd: discoverd.DefaultClient,
		},
	}

	if err := r.Start(); err != nil {
		shutdown.Fatal(err)
	}
	shutdown.BeforeExit(r.Close)

	listener, err := listenFunc("tcp4", *apiAddr)
	if err != nil {
		shutdown.Fatal(listenErr{*apiAddr, err})
	}

	services := map[string]string{
		"router-api":  *apiAddr,
		"router-http": *httpAddr,
	}
	for service, addr := range services {
		hb, err := discoverd.AddServiceAndRegister(service, addr)
		if err != nil {
			shutdown.Fatal(err)
		}
		shutdown.BeforeExit(func() { hb.Close() })
	}

	shutdown.Fatal(http.Serve(listener, apiHandler(&r)))
}

type listenErr struct {
	Addr string
	Err  error
}

func (e listenErr) Error() string {
	return fmt.Sprintf("error binding to port (check if another service is listening on %s): %s", e.Addr, e.Err)
}
