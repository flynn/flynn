package server

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/flynn/flynn/discoverd/client"
	dt "github.com/flynn/flynn/discoverd/types"
	hh "github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/sse"
	"github.com/flynn/flynn/pkg/status"
	"github.com/flynn/flynn/pkg/stream"
	log "github.com/inconshreveable/log15"
	"github.com/julienschmidt/httprouter"
)

// StreamBufferSize is the size of the channel buffer used for event subscription.
const StreamBufferSize = 64 // TODO: Figure out how big this buffer should be.

func loggerFn(handler http.Handler, logger log.Logger, clientIP string, rw *hh.ResponseWriter, req *http.Request) {
	start := time.Now()
	handler.ServeHTTP(rw, req)
	switch rw.Status() {
	case 200, 307, 0: // 0 == 200 OK
	default:
		logger.Info("request completed", "method", req.Method, "path", req.URL.Path, "client_ip", clientIP, "status", rw.Status(), "duration", time.Since(start))
	}
}

// NewHandler returns a new instance of Handler.
func NewHandler(proxy bool, peers []string) *Handler {
	r := httprouter.New()

	h := &Handler{Handler: r}
	h.Shutdown.Store(false)

	h.Peers = peers
	h.Proxy.Store(proxy)

	if os.Getenv("DEBUG") != "" {
		h.Handler = hh.ContextInjector("discoverd", hh.NewRequestLoggerCustom(h.Handler, loggerFn))
	}

	r.HandlerFunc("GET", status.Path, status.HealthyHandler.ServeHTTP)

	r.PUT("/services/:service", h.servePutService)
	r.DELETE("/services/:service", h.serveDeleteService)
	r.GET("/services/:service", h.serveGetService)

	r.PUT("/services/:service/meta", h.servePutServiceMeta)
	r.GET("/services/:service/meta", h.serveGetServiceMeta)

	r.PUT("/services/:service/instances/:instance_id", h.servePutInstance)
	r.DELETE("/services/:service/instances/:instance_id", h.serveDeleteInstance)
	r.GET("/services/:service/instances", h.serveGetInstances)

	r.PUT("/services/:service/leader", h.servePutLeader)
	r.GET("/services/:service/leader", h.serveGetLeader)

	r.GET("/raft/leader", h.serveGetRaftLeader)
	r.GET("/raft/peers", h.serveGetRaftPeers)
	r.PUT("/raft/peers/:peer", h.servePutRaftPeer)
	r.DELETE("/raft/peers/:peer", h.serveDeleteRaftPeer)
	r.POST("/raft/promote", h.servePromote)
	r.POST("/raft/demote", h.serveDemote)

	r.GET("/ping", h.servePing)

	r.POST("/shutdown", h.serveShutdown)
	return h
}

// Handler represents an HTTP handler for the Store.
type Handler struct {
	http.Handler
	Shutdown atomic.Value // bool
	Proxy    atomic.Value // bool
	Main     interface {
		Deregister() error
		Close() (dt.TargetLogIndex, error)
		Promote() error
		Demote() error
	}
	Store interface {
		Leader() string
		AddService(service string, config *discoverd.ServiceConfig) error
		RemoveService(service string) error
		SetServiceMeta(service string, meta *discoverd.ServiceMeta) error
		ServiceMeta(service string) *discoverd.ServiceMeta
		AddInstance(service string, inst *discoverd.Instance) error
		RemoveInstance(service, id string) error
		Instances(service string) ([]*discoverd.Instance, error)
		Config(service string) *discoverd.ServiceConfig
		SetServiceLeader(service, id string) error
		ServiceLeader(service string) (*discoverd.Instance, error)
		Subscribe(service string, sendCurrent bool, kinds discoverd.EventKind, ch chan *discoverd.Event) stream.Stream

		AddPeer(peer string) error
		RemovePeer(peer string) error
		GetPeers() ([]string, error)
		LastIndex() uint64
	}
	Peers []string
}

// Whitelisted endpoints won't be proxied.
func proxyWhitelisted(r *http.Request) bool {
	for _, url := range []string{"/raft/promote", "/raft/demote", "/shutdown"} {
		if strings.HasPrefix(r.URL.Path, url) {
			return true
		}
	}
	return false
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.Shutdown.Load().(bool) {
		hh.ServiceUnavailableError(w, "discoverd: shutting down")
		return
	}
	// If running in proxy mode then redirect requests to a random peer
	if h.Proxy.Load().(bool) {
		if !proxyWhitelisted(r) {
			// TODO(jpg): Should configuring the peer in proxy mode with no peers be impossible?
			host := h.Peers[rand.Intn(len(h.Peers))]
			redirectToHost(w, r, host)
			return
		}
	} else {
		// Send current peer list and index to the client so it can keep the list of
		// peers in sync with the cluster.
		peers, err := h.Store.GetPeers()
		if err == nil {
			w.Header().Set("Discoverd-Peers", strings.Join(peers, ","))
		}
		w.Header().Set("Discoverd-Index", strconv.FormatUint(h.Store.LastIndex(), 10))
	}
	h.Handler.ServeHTTP(w, r)
	return
}

// servePutService creates a service.
func (h *Handler) servePutService(w http.ResponseWriter, r *http.Request, params httprouter.Params) {
	// Retrieve the path parameter.
	service := params.ByName("service")
	if err := ValidServiceName(service); err != nil {
		hh.ValidationError(w, "", err.Error())
		return
	}

	// Read config from the request.
	config := &discoverd.ServiceConfig{}
	if err := hh.DecodeJSON(r, config); err != nil {
		hh.Error(w, err)
		return
	}

	// Add the service to the store.
	if err := h.Store.AddService(service, config); err == ErrNotLeader {
		h.redirectToLeader(w, r)
		return
	} else if IsServiceExists(err) {
		hh.ObjectExistsError(w, err.Error())
		return
	} else if err != nil {
		hh.Error(w, err)
		return
	}
}

// serveDeleteService removes a service from the store by name.
func (h *Handler) serveDeleteService(w http.ResponseWriter, r *http.Request, params httprouter.Params) {
	// Retrieve the path parameter.
	service := params.ByName("service")
	if err := ValidServiceName(service); err != nil {
		hh.ValidationError(w, "", err.Error())
		return
	}

	// Delete from the store.
	if err := h.Store.RemoveService(params.ByName("service")); err == ErrNotLeader {
		h.redirectToLeader(w, r)
		return
	} else if IsNotFound(err) {
		hh.ObjectNotFoundError(w, err.Error())
		return
	} else if err != nil {
		hh.Error(w, err)
		return
	}
}

// serveGetService streams service events to the client.
func (h *Handler) serveGetService(w http.ResponseWriter, r *http.Request, params httprouter.Params) {
	// This should only return a stream if the Accept header is
	// text/event-stream (and return a 406 otherwise), but we
	// always return a stream due to Go's http.Client not
	// maintaining headers through a redirect.
	//
	// See https://github.com/flynn/flynn/issues/1880
	h.serveStream(w, params, discoverd.EventKindAll)
}

// serveServiceMeta sets the metadata for a service.
func (h *Handler) servePutServiceMeta(w http.ResponseWriter, r *http.Request, params httprouter.Params) {
	// Read the metadata from the request.
	meta := &discoverd.ServiceMeta{}
	if err := hh.DecodeJSON(r, meta); err != nil {
		hh.Error(w, err)
		return
	}

	// Update the meta in the store.
	if err := h.Store.SetServiceMeta(params.ByName("service"), meta); err == ErrNotLeader {
		h.redirectToLeader(w, r)
		return
	} else if IsNotFound(err) {
		hh.ObjectNotFoundError(w, err.Error())
		return
	} else if err != nil {
		hh.Error(w, err)
		return
	}

	// Write meta back to response.
	hh.JSON(w, 200, meta)
}

// serveGetServiceMeta returns the metadata for a service.
func (h *Handler) serveGetServiceMeta(w http.ResponseWriter, r *http.Request, params httprouter.Params) {
	// Read path parameter.
	service := params.ByName("service")

	// Read meta from the store.
	meta := h.Store.ServiceMeta(service)
	if meta == nil {
		hh.ObjectNotFoundError(w, "service meta not found")
		return
	}

	// Write meta to the response.
	hh.JSON(w, 200, meta)
}

// servePutInstance adds an instance to a service.
func (h *Handler) servePutInstance(w http.ResponseWriter, r *http.Request, params httprouter.Params) {
	// Read path parameter.
	service := params.ByName("service")

	// Read instance from request.
	inst := &discoverd.Instance{}
	if err := json.NewDecoder(r.Body).Decode(inst); err != nil {
		hh.Error(w, err)
		return
	}

	// Ensure instance is valid.
	if err := inst.Valid(); err != nil {
		hh.ValidationError(w, "", err.Error())
		return
	}

	// Add instance to service in the store.
	if err := h.Store.AddInstance(service, inst); err == ErrNotLeader {
		h.redirectToLeader(w, r)
		return
	} else if IsNotFound(err) {
		hh.ObjectNotFoundError(w, err.Error())
		return
	} else if err != nil {
		hh.Error(w, err)
		return
	}
}

// serveDeleteInstance removes an instance from the store by name.
func (h *Handler) serveDeleteInstance(w http.ResponseWriter, r *http.Request, params httprouter.Params) {
	// Retrieve path parameters.
	service := params.ByName("service")
	instanceID := params.ByName("instance_id")

	// Remove instance from the store.
	if err := h.Store.RemoveInstance(service, instanceID); err == ErrNotLeader {
		h.redirectToLeader(w, r)
		return
	} else if IsNotFound(err) {
		hh.ObjectNotFoundError(w, err.Error())
		return
	} else if err != nil {
		hh.Error(w, err)
		return
	}
}

// serveGetInstances returns a list of all instances for a service.
func (h *Handler) serveGetInstances(w http.ResponseWriter, r *http.Request, params httprouter.Params) {
	// If the client is requesting a stream, then handle as a stream.
	if strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
		h.serveStream(w, params, discoverd.EventKindUp|discoverd.EventKindUpdate|discoverd.EventKindDown)
		return
	}

	// Otherwise read instances from the store.
	instances, err := h.Store.Instances(params.ByName("service"))
	if err != nil {
		hh.Error(w, err)
		return
	} else if instances == nil {
		hh.ObjectNotFoundError(w, fmt.Sprintf("service not found: %q", params.ByName("service")))
		return
	}

	// Write instances to the response.
	hh.JSON(w, 200, instances)
}

// servePutLeader sets the leader for a service.
func (h *Handler) servePutLeader(w http.ResponseWriter, r *http.Request, params httprouter.Params) {
	// Retrieve path parameters.
	service := params.ByName("service")

	// Check if the service allows manual leader election.
	config := h.Store.Config(service)
	if config == nil || config.LeaderType != discoverd.LeaderTypeManual {
		hh.ValidationError(w, "", "service leader election type is not manual")
		return
	}

	// Read instance from the request.
	inst := &discoverd.Instance{}
	if err := hh.DecodeJSON(r, inst); err != nil {
		hh.Error(w, err)
		return
	}

	// Manually set the leader on the service.
	if err := h.Store.SetServiceLeader(service, inst.ID); err == ErrNotLeader {
		h.redirectToLeader(w, r)
		return
	} else if err != nil {
		hh.Error(w, err)
		return
	}
}

// serveGetLeader returns the current leader for a service.
func (h *Handler) serveGetLeader(w http.ResponseWriter, r *http.Request, params httprouter.Params) {
	// Process as a stream if that's what the client wants.
	if strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
		h.serveStream(w, params, discoverd.EventKindLeader)
		return
	}

	// Otherwise retrieve the current leader.
	service := params.ByName("service")
	leader, err := h.Store.ServiceLeader(service)
	if err != nil {
		hh.Error(w, err)
		return
	} else if leader == nil {
		hh.ObjectNotFoundError(w, "no leader found")
		return
	}

	// Write leader to the response.
	hh.JSON(w, 200, leader)
}

// servePing returns a 200 OK.
func (h *Handler) servePing(w http.ResponseWriter, r *http.Request, params httprouter.Params) {}

func (h *Handler) serveShutdown(w http.ResponseWriter, r *http.Request, params httprouter.Params) {
	// deregister the server before marking as shutdown as deregistration
	// is performed using the HTTP server
	if err := h.Main.Deregister(); err != nil {
		hh.Error(w, err)
		return
	}
	h.Shutdown.Store(true)
	targetLogIndex, err := h.Main.Close()
	if err != nil {
		hh.Error(w, err)
		return
	}
	hh.JSON(w, 200, targetLogIndex)
}

// servePromote attempts to promote this discoverd peer to a raft peer
func (h *Handler) servePromote(w http.ResponseWriter, r *http.Request, params httprouter.Params) {
	if err := h.Main.Promote(); err != nil {
		hh.Error(w, err)
		return
	}
}

// serveDemote attempts to demote this peer from a raft peer to a proxy
func (h *Handler) serveDemote(w http.ResponseWriter, r *http.Request, params httprouter.Params) {
	if err := h.Main.Demote(); err != nil {
		hh.Error(w, err)
		return
	}
}

// serveStream creates a subscription and streams out events in SSE format.
func (h *Handler) serveStream(w http.ResponseWriter, params httprouter.Params, kind discoverd.EventKind) {
	// Create a buffered channel to receive events.
	ch := make(chan *discoverd.Event, StreamBufferSize)

	// Subscribe to events on the store.
	service := params.ByName("service")
	stream := h.Store.Subscribe(service, true, kind, ch)

	// Create and serve an SSE stream.
	s := sse.NewStream(w, ch, nil)
	s.Serve()
	s.Wait()
	stream.Close()

	// Check if there was an error while closing.
	if err := stream.Err(); err != nil {
		s.CloseWithError(err)
	}
}

// serveGetRaftLeader returns the current raft leader.
func (h *Handler) serveGetRaftLeader(w http.ResponseWriter, r *http.Request, params httprouter.Params) {
	leader := h.Store.Leader()
	if leader == "" {
		hh.ServiceUnavailableError(w, ErrNoKnownLeader.Error())
		return
	}

	hh.JSON(w, 200, dt.RaftLeader{Host: h.Store.Leader()})
}

// serveGetRaftPeers returns the current raft peers.
func (h *Handler) serveGetRaftPeers(w http.ResponseWriter, r *http.Request, params httprouter.Params) {
	peers, err := h.Store.GetPeers()
	if err != nil {
		hh.Error(w, err)
	}

	hh.JSON(w, 200, peers)
}

// servePutRaftNodes joins a peer to the store cluster.
func (h *Handler) servePutRaftPeer(w http.ResponseWriter, r *http.Request, params httprouter.Params) {
	peer := params.ByName("peer")
	if err := h.Store.AddPeer(peer); err == ErrNotLeader {
		h.redirectToLeader(w, r)
		return
	} else if err != nil {
		hh.Error(w, err)
		return
	}
	var targetLogIndex dt.TargetLogIndex
	targetLogIndex.LastIndex = h.Store.LastIndex()
	hh.JSON(w, 200, targetLogIndex)
}

// serveDeleteRaftNodes removes a peer to the store cluster.
func (h *Handler) serveDeleteRaftPeer(w http.ResponseWriter, r *http.Request, params httprouter.Params) {
	peer := params.ByName("peer")
	if err := h.Store.RemovePeer(peer); err == ErrNotLeader {
		h.redirectToLeader(w, r)
		return
	} else if err != nil {
		hh.Error(w, err)
		return
	}
}

// redirectToLeader redirects the request to the current known leader.
func (h *Handler) redirectToLeader(w http.ResponseWriter, r *http.Request) {
	// Find the current leader.
	leader := h.Store.Leader()
	if leader == "" {
		hh.ServiceUnavailableError(w, ErrNoKnownLeader.Error())
		return
	}

	redirectToHost(w, r, leader)
}

func redirectToHost(w http.ResponseWriter, r *http.Request, hostport string) {
	// Create the redirection URL.
	u := *r.URL
	if r.TLS == nil {
		u.Scheme = "http"
	} else {
		u.Scheme = "https"
	}

	// Assume the host port is the same as this handler.
	host, _, _ := net.SplitHostPort(hostport)
	_, port, _ := net.SplitHostPort(r.Host)
	u.Host = net.JoinHostPort(host, port)

	// Redirect request to new host.
	http.Redirect(w, r, u.String(), http.StatusTemporaryRedirect)
}
