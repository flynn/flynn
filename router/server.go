package main

import (
	"crypto/tls"
	"encoding/base64"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/keepalive"
	"github.com/flynn/flynn/pkg/postgres"
	"github.com/flynn/flynn/pkg/shutdown"
	"github.com/flynn/flynn/router/schema"
	"github.com/flynn/flynn/router/types"
	"github.com/inconshreveable/log15"
)

var logger = log15.New()

func init() {
	if os.Getenv("DEBUG") == "" {
		// filter debug log messages if DEBUG is not set
		logger.SetHandler(log15.LvlFilterHandler(log15.LvlInfo, log15.StdoutHandler))
	}
}

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

func (s *Router) ListenerFor(typ string) Listener {
	switch typ {
	case "http":
		return s.HTTP
	case "tcp":
		return s.TCP
	default:
		return nil
	}
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
		s.HTTP.Close()
		return err
	}
	return nil
}

func (s *Router) Close() {
	s.HTTP.Close()
	s.TCP.Close()
}

var listenFunc = keepalive.ReusableListen

func main() {
	defer shutdown.Exit()
	log := logger.New("fn", "main")

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
	proxyProtocol := os.Getenv("PROXY_PROTOCOL") == "true"
	legacyTLS := os.Getenv("LEGACY_TLS") == "true"

	httpPort := flag.Int("http-port", 8080, "default http listen port")
	httpsPort := flag.Int("https-port", 4433, "default https listen port")
	tcpIP := flag.String("tcp-ip", os.Getenv("LISTEN_IP"), "tcp router listen ip")
	tcpRangeStart := flag.Int("tcp-range-start", 3000, "tcp port range start")
	tcpRangeEnd := flag.Int("tcp-range-end", 3500, "tcp port range end")
	certFile := flag.String("tls-cert", "", "TLS (SSL) cert file in pem format")
	keyFile := flag.String("tls-key", "", "TLS (SSL) key file in pem format")
	apiPort := flag.String("api-port", "", "api listen port")
	flag.Parse()

	httpPorts := []int{*httpPort}
	httpsPorts := []int{*httpsPort}
	if portRaw := os.Getenv("DEFAULT_HTTP_PORT"); portRaw != "" {
		if port, err := strconv.Atoi(portRaw); err != nil {
			shutdown.Fatalf("Invalid DEFAULT_HTTP_PORTS: %s", err)
		} else if port == 0 {
			log.Warn("Disabling HTTP acccess (DEFAULT_HTTP_PORT=0)")
			httpPorts = nil
		} else {
			httpPorts[0] = port
		}
	}
	if portRaw := os.Getenv("DEFAULT_HTTPS_PORT"); portRaw != "" {
		if port, err := strconv.Atoi(portRaw); err != nil {
			shutdown.Fatalf("Invalid DEFAULT_HTTPS_PORTS: %s", err)
		} else if port == 0 {
			shutdown.Fatal("Cannot disable HTTPS access (DEFAULT_HTTPS_PORT=0)")
		} else {
			httpsPorts[0] = port
		}
	}
	defaultPorts := append(httpPorts, httpsPorts...)
	if added := os.Getenv("ADDITIONAL_HTTP_PORTS"); added != "" {
		for _, raw := range strings.Split(added, ",") {
			if port, err := strconv.Atoi(raw); err == nil {
				httpPorts = append(httpPorts, port)
			} else {
				shutdown.Fatal(err)
			}
		}
	}
	if added := os.Getenv("ADDITIONAL_HTTPS_PORTS"); added != "" {
		for _, raw := range strings.Split(added, ",") {
			if port, err := strconv.Atoi(raw); err == nil {
				httpsPorts = append(httpsPorts, port)
			} else {
				shutdown.Fatal(err)
			}
		}
	}

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

	var error503Page []byte
	if error503PageURL := os.Getenv("ERROR_503_PAGE_URL"); error503PageURL != "" {
		func() {
			res, err := http.Get(error503PageURL)
			if err != nil {
				log.Error("error getting ERROR_503_PAGE_URL", "err", err)
				return
			}
			defer res.Body.Close()
			if res.StatusCode != 200 {
				log.Error("unexpected status code getting ERROR_503_PAGE_URL", "status", res.StatusCode)
				return
			}
			error503Page, err = ioutil.ReadAll(&io.LimitedReader{R: res.Body, N: 1000000})
			if err != nil {
				log.Error("error reading ERROR_503_PAGE_URL", "err", err)
				return
			}
			return

		}()
	}

	log.Info("connecting to postgres")
	db := postgres.Wait(nil, nil)

	log.Info("running DB migrations")
	if err := migrateDB(db); err != nil {
		shutdown.Fatal(err)
	}
	db.Close()

	log.Info("reconnecting to postgres with prepared queries")
	db = postgres.Wait(nil, schema.PrepareStatements)

	shutdown.BeforeExit(func() { db.Close() })

	var httpAddrs []string
	var httpsAddrs []string
	var reservedPorts []int
	for _, port := range httpPorts {
		httpAddrs = append(httpAddrs, net.JoinHostPort(os.Getenv("LISTEN_IP"), strconv.Itoa(port)))
		reservedPorts = append(reservedPorts, port)
	}
	for _, port := range httpsPorts {
		httpsAddrs = append(httpsAddrs, net.JoinHostPort(os.Getenv("LISTEN_IP"), strconv.Itoa(port)))
		reservedPorts = append(reservedPorts, port)
	}
	r := Router{
		TCP: &TCPListener{
			IP:            *tcpIP,
			startPort:     *tcpRangeStart,
			endPort:       *tcpRangeEnd,
			ds:            NewPostgresDataStore("tcp", db.ConnPool),
			discoverd:     discoverd.DefaultClient,
			reservedPorts: reservedPorts,
		},
		HTTP: &HTTPListener{
			Addrs:             httpAddrs,
			TLSAddrs:          httpsAddrs,
			LegacyTLSVersions: legacyTLS,
			defaultPorts:      defaultPorts,
			cookieKey:         cookieKey,
			keypair:           keypair,
			ds:                NewPostgresDataStore("http", db.ConnPool),
			discoverd:         discoverd.DefaultClient,
			proxyProtocol:     proxyProtocol,
			error503Page:      error503Page,
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

	httpAddr := net.JoinHostPort(os.Getenv("LISTEN_IP"), strconv.Itoa(httpPorts[0]))
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
