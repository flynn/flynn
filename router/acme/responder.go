package acme

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"

	acme "github.com/eggsampler/acme/v3"
	ct "github.com/flynn/flynn/controller/types"
	discoverd "github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/httphelper"
	router "github.com/flynn/flynn/router/types"
	"github.com/inconshreveable/log15"
)

const (
	responderAppName     = "router"
	responderServiceName = "router-acme-responder"
	defaultResponderPort = "8080"
)

// Responder responds to ACME authz challenges
type Responder struct {
	client ControllerClient

	challengeMtx sync.RWMutex
	challenges   map[string]map[string]*acme.Challenge

	ln net.Listener
	hb discoverd.Heartbeater

	log log15.Logger
}

// NewResponder returns a responder that responds to authz challenges using the
// given controller and discoverd clients, listening for http-01 challenges at
// the given httpAddr
func NewResponder(client ControllerClient, discoverdClient *discoverd.Client, httpAddr string, log log15.Logger) (*Responder, error) {
	log.Info("starting responder HTTP server", "addr", httpAddr)
	ln, err := net.Listen("tcp", httpAddr)
	if err != nil {
		log.Error("error starting responder HTTP server", "err", err)
		return nil, err
	}
	hb, err := discoverdClient.AddServiceAndRegisterInstance(responderServiceName, &discoverd.Instance{
		Addr:  fmt.Sprintf(":%d", ln.Addr().(*net.TCPAddr).Port),
		Proto: "http",
	})
	if err != nil {
		log.Error("error starting responder HTTP server", "err", err)
		return nil, err
	}
	r := &Responder{
		client:     client,
		challenges: make(map[string]map[string]*acme.Challenge),
		ln:         ln,
		hb:         hb,
		log:        log,
	}
	handler := httphelper.ContextInjector("acme-responder", httphelper.NewRequestLogger(r))
	go http.Serve(ln, handler)
	return r, nil
}

// RespondHTTP01 responds to a http-01 ACME challenge by creating a route for
// the given certificate's domain pointing at the responder's HTTP handler,
// completing the challenge and then removing the route
func (r *Responder) RespondHTTP01(cert *ct.ManagedCertificate, challenge *acme.Challenge, complete func() error) error {
	route, err := r.createChallengeRoute(cert, challenge)
	if err != nil {
		return err
	}
	if err := complete(); err != nil {
		return err
	}
	return r.deleteChallengeRoute(route, challenge)
}

// ServeHTTP serves http-01 challenge responses
func (r *Responder) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if !strings.HasPrefix(req.URL.Path, "/.well-known/acme-challenge/") {
		http.NotFound(w, req)
		return
	}
	token := strings.TrimPrefix(req.URL.Path, "/.well-known/acme-challenge/")
	if token == "" {
		http.NotFound(w, req)
		return
	}
	domain := req.Host
	if host, _, err := net.SplitHostPort(domain); err == nil {
		domain = host
	}
	challenge, ok := r.findChallenge(domain, token)
	if !ok {
		http.NotFound(w, req)
		return
	}
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, challenge.KeyAuthorization)
}

// Close closes the responder's discoverd registration and HTTP listener
func (r *Responder) Close() error {
	r.hb.Close()
	return r.ln.Close()
}

func (r *Responder) findChallenge(domain, token string) (*acme.Challenge, bool) {
	r.challengeMtx.RLock()
	defer r.challengeMtx.RUnlock()
	challenges, ok := r.challenges[domain]
	if !ok {
		return nil, false
	}
	challenge, ok := challenges[token]
	return challenge, ok
}

func (r *Responder) createChallengeRoute(cert *ct.ManagedCertificate, challenge *acme.Challenge) (*router.Route, error) {
	// add the challenge to r.challenges
	r.challengeMtx.Lock()
	challenges, ok := r.challenges[cert.Domain]
	if !ok {
		challenges = make(map[string]*acme.Challenge)
		r.challenges[cert.Domain] = challenges
	}
	challenges[challenge.Token] = challenge
	r.challengeMtx.Unlock()

	// create a route
	route := (&router.HTTPRoute{
		Domain:  cert.Domain,
		Path:    "/.well-known/acme-challenge/",
		Service: responderServiceName,
	}).ToRoute()
	if err := r.client.CreateRoute(responderAppName, route); err != nil {
		return nil, err
	}
	return route, nil
}

func (r *Responder) deleteChallengeRoute(route *router.Route, challenge *acme.Challenge) error {
	// delete the route
	if err := r.client.DeleteRoute(responderAppName, route.FormattedID()); err != nil {
		return err
	}

	// remove the challenge from r.challenges
	r.challengeMtx.Lock()
	defer r.challengeMtx.Unlock()
	challenges, ok := r.challenges[route.Domain]
	if !ok {
		return nil
	}
	delete(challenges, challenge.Token)
	if len(challenges) == 0 {
		delete(r.challenges, route.Domain)
	}
	return nil
}
