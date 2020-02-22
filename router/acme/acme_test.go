package acme

import (
	"errors"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	acme "github.com/eggsampler/acme/v3"
	"github.com/flynn/flynn/controller/api"
	ct "github.com/flynn/flynn/controller/types"
	discoverd "github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/discoverd/testutil"
	"github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/stream"
	"github.com/flynn/flynn/router/proxy"
	router "github.com/flynn/flynn/router/types"
	"github.com/inconshreveable/log15"
	"github.com/letsencrypt/pebble/ca"
	"github.com/letsencrypt/pebble/db"
	"github.com/letsencrypt/pebble/va"
	"github.com/letsencrypt/pebble/wfe"
	"github.com/miekg/dns"
)

func TestHTTP01Challenge(t *testing.T) {
	// start a test discoverd
	discoverd, killDiscoverd := testutil.BootDiscoverd(t, "")
	defer killDiscoverd()

	// create a test router
	router := newTestRouter(discoverd)
	defer router.Close()

	// create a test controller
	controller := newTestController(router)
	defer controller.Close()

	// create a test responder
	responder, err := NewResponder(controller, discoverd, ":0", log15.New())
	if err != nil {
		t.Fatal(err)
	}
	defer responder.Close()

	// start a test acme server
	srv, account := newTestACMEServer(t, router)
	defer srv.Close()

	// run the ACME service
	service, err := srv.acme.NewService(account, controller, responder)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Start(); err != nil {
		t.Fatal(err)
	}
	defer service.Stop()

	// watch for certificate changes
	certs := make(chan *ct.ManagedCertificate, 1)
	stream, err := controller.StreamManagedCertificates(certs)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()

	// add a managed certificate
	domain := "example.com"
	controller.addCert(&ct.ManagedCertificate{Domain: domain})

	// wait for the managed certificate to be issued
	var managedCert *ct.ManagedCertificate
	timeout := time.After(10 * time.Second)
loop:
	for {
		select {
		case cert, ok := <-certs:
			if !ok {
				t.Fatalf("error waiting for certificate events: %s", stream.Err())
			}
			if cert.Domain == domain && cert.Status == ct.ManagedCertificateStatusIssued {
				managedCert = cert
				break loop
			}
		case <-timeout:
			t.Fatal("timed out waiting for managed certificate to be issued")
		}
	}

	// check the issued certificate
	cert := managedCert.Certificate
	if cert == nil {
		t.Fatal("expected CurrentCertificate to be set")
	}
	staticCert := api.NewStaticCertificate(cert)
	if staticCert.Status != api.StaticCertificate_STATUS_VALID {
		t.Fatalf("issued certificate is not valid: %s: %s", staticCert.Status, staticCert.StatusDetail)
	}
}

const testContact = "mailto:acme@example.com"

func newTestACMEServer(t *testing.T, router *testRouter) (*testACMEServer, *Account) {
	// start a DNS server
	dnsConn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	dnsMux := dns.NewServeMux()
	dnsMux.HandleFunc("example.com.", func(w dns.ResponseWriter, req *dns.Msg) {
		res := &dns.Msg{}
		res.Authoritative = true
		res.Compress = true
		res.SetReply(req)
		res.Answer = []dns.RR{&dns.A{
			Hdr: dns.RR_Header{
				Name:   req.Question[0].Name,
				Rrtype: dns.TypeA,
				Class:  dns.ClassINET,
			},
			A: router.Listener.Addr().(*net.TCPAddr).IP,
		}}
		w.WriteMsg(res)
	})
	dnsSrv := &dns.Server{
		PacketConn: dnsConn,
		Handler:    dnsMux,
	}
	go dnsSrv.ActivateAndServe()

	// start a pebble CA server
	routerPort := router.Listener.Addr().(*net.TCPAddr).Port
	logger := log.New(os.Stdout, "Pebble ", log.LstdFlags)
	db := db.NewMemoryStore()
	ca := ca.New(logger, db, "", 0)
	va := va.New(logger, routerPort, routerPort, false, dnsConn.LocalAddr().String())
	wfeImpl := wfe.New(logger, db, va, ca, false, false)
	httpSrv := httptest.NewTLSServer(wfeImpl.Handler())

	// create an account
	directoryURL := httpSrv.URL + wfe.DirectoryPath
	acme, err := New(directoryURL, log15.New(), acme.WithHTTPClient(httpSrv.Client()))
	if err != nil {
		t.Fatal(err)
	}
	account := &Account{
		Contacts:             []string{testContact},
		TermsOfServiceAgreed: true,
	}
	if err := acme.CreateAccount(account); err != nil {
		t.Fatal(err)
	}

	// return the test server amd account
	return &testACMEServer{
		httpSrv: httpSrv,
		dnsSrv:  dnsSrv,
		acme:    acme,
	}, account
}

type testACMEServer struct {
	httpSrv *httptest.Server
	dnsSrv  *dns.Server
	acme    *ACME
	account *Account
}

func (t *testACMEServer) Close() {
	t.httpSrv.Close()
	t.dnsSrv.Shutdown()
}

func newTestController(router *testRouter) *testController {
	return &testController{
		router: router,
		subs:   make(map[chan *ct.ManagedCertificate]struct{}),
		stop:   make(chan struct{}),
	}
}

type testController struct {
	router *testRouter
	mtx    sync.RWMutex
	subs   map[chan *ct.ManagedCertificate]struct{}
	stop   chan struct{}
}

func (t *testController) StreamManagedCertificates(certs chan *ct.ManagedCertificate) (stream.Stream, error) {
	s := stream.New()
	t.mtx.Lock()
	t.subs[certs] = struct{}{}
	t.mtx.Unlock()
	go func() {
		defer close(certs)
		select {
		case <-s.StopCh:
			// drain certs to avoid deadlock
			go func() {
				for range certs {
				}
			}()
			// unsubscribe the channel
			t.mtx.Lock()
			delete(t.subs, certs)
			t.mtx.Unlock()
		case <-t.stop:
			s.Error = errors.New("store closed")
			return
		}
	}()
	return s, nil
}

func (t *testController) UpdateManagedCertificate(cert *ct.ManagedCertificate) error {
	t.mtx.RLock()
	defer t.mtx.RUnlock()
	for certs := range t.subs {
		certs <- cert
	}
	return nil
}

func (t *testController) CreateRoute(appID string, route *router.Route) error {
	t.router.addRoute(route)
	return nil
}

func (t *testController) DeleteRoute(appID string, routeID string) error {
	return nil
}

func (t *testController) addCert(cert *ct.ManagedCertificate) {
	cert.Status = ct.ManagedCertificateStatusPending
	t.UpdateManagedCertificate(cert)
}

func (t *testController) Close() {
	t.router.Close()
	close(t.stop)
}

type testRouter struct {
	*httptest.Server

	discoverd *discoverd.Client

	proxies map[string]*proxy.ReverseProxy
}

func newTestRouter(discoverd *discoverd.Client) *testRouter {
	r := &testRouter{
		discoverd: discoverd,
		proxies:   make(map[string]*proxy.ReverseProxy),
	}
	handler := httphelper.ContextInjector("test-router", httphelper.NewRequestLogger(r))
	r.Server = httptest.NewServer(handler)
	return r
}

func (t *testRouter) addRoute(route *router.Route) {
	t.proxies[route.Domain] = proxy.NewReverseProxy(proxy.ReverseProxyConfig{
		BackendListFunc: t.backendListFunc(route.Service),
		RequestTracker:  &testRequestTracker{},
		Logger:          log15.New(),
	})
}

func (t *testRouter) backendListFunc(service string) proxy.BackendListFunc {
	return func() []*router.Backend {
		instances, err := t.discoverd.Instances(service, time.Second)
		if err != nil {
			return nil
		}
		backends := make([]*router.Backend, len(instances))
		for i, inst := range instances {
			backends[i] = &router.Backend{
				Service: service,
				Addr:    inst.Addr,
			}
		}
		return backends
	}
}

func (t *testRouter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	host, _, _ := net.SplitHostPort(r.Host)
	proxy, ok := t.proxies[host]
	if !ok {
		http.NotFound(w, r)
		return
	}
	proxy.ServeHTTP(w, r)
}

type testRequestTracker struct{}

func (testRequestTracker) TrackRequestStart(backend string) {}
func (testRequestTracker) TrackRequestDone(backend string)  {}
