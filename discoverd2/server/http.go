package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/julienschmidt/httprouter"
	"github.com/flynn/flynn/discoverd2/client"
	"github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/sse"
	"github.com/flynn/flynn/pkg/stream"
)

type Datastore interface {
	// Typically implemented by a Backend
	AddService(service string) error
	RemoveService(service string) error
	AddInstance(service string, inst *discoverd.Instance) error
	RemoveInstance(service, id string) error

	// Typically implemented by State
	Get(service string) []*discoverd.Instance
	GetLeader(service string) *discoverd.Instance
	Subscribe(service string, sendCurrent bool, kinds discoverd.EventKind, ch chan *discoverd.Event) stream.Stream
}

type basicDatastore struct {
	*State
	Backend
}

func (d basicDatastore) AddService(service string) error {
	return d.Backend.AddService(service)
}

func (d basicDatastore) RemoveService(service string) error {
	return d.Backend.RemoveService(service)
}

func (d basicDatastore) AddInstance(service string, inst *discoverd.Instance) error {
	return d.Backend.AddInstance(service, inst)
}

func (d basicDatastore) RemoveInstance(service, id string) error {
	return d.Backend.RemoveInstance(service, id)
}

func NewBasicDatastore(state *State, backend Backend) Datastore {
	return &basicDatastore{state, backend}
}

func NewHTTPHandler(ds Datastore) http.Handler {
	router := httprouter.New()

	api := &httpAPI{
		Store: ds,
	}

	router.PUT("/services/:service", api.AddService)
	router.DELETE("/services/:service", api.RemoveService)
	router.GET("/services/:service", api.GetServiceStream)

	router.PUT("/services/:service/instances/:instance_id", api.AddInstance)
	router.DELETE("/services/:service/instances/:instance_id", api.RemoveInstance)
	router.GET("/services/:service/instances", api.GetInstances)

	router.GET("/services/:service/leader", api.GetLeader)

	return router
}

type httpAPI struct {
	Store Datastore
}

func jsonError(w http.ResponseWriter, code int, err error) {
	httphelper.JSON(w, code, struct {
		Error string `json:"error"`
	}{err.Error()})
}

func (h *httpAPI) AddService(w http.ResponseWriter, r *http.Request, params httprouter.Params) {
	service := params.ByName("service")
	if err := ValidServiceName(service); err != nil {
		jsonError(w, 400, err)
		return
	}
	if err := h.Store.AddService(service); err != nil {
		if IsServiceExists(err) {
			jsonError(w, 400, err)
		} else {
			jsonError(w, 500, err)
		}
		return
	}
}

func (h *httpAPI) RemoveService(w http.ResponseWriter, r *http.Request, params httprouter.Params) {
	service := params.ByName("service")
	if err := ValidServiceName(service); err != nil {
		jsonError(w, 400, err)
		return
	}
	if err := h.Store.RemoveService(params.ByName("service")); err != nil {
		if IsNotFound(err) {
			jsonError(w, 404, err)
		} else {
			jsonError(w, 500, err)
		}
		return
	}
}

func (h *httpAPI) AddInstance(w http.ResponseWriter, r *http.Request, params httprouter.Params) {
	inst := &discoverd.Instance{}
	if err := json.NewDecoder(r.Body).Decode(inst); err != nil {
		jsonError(w, 400, err)
		return
	}
	if err := inst.Valid(); err != nil {
		jsonError(w, 400, err)
		return
	}
	if err := h.Store.AddInstance(params.ByName("service"), inst); err != nil {
		if IsNotFound(err) {
			jsonError(w, 404, err)
		} else {
			jsonError(w, 500, err)
		}
		return
	}
}

func (h *httpAPI) RemoveInstance(w http.ResponseWriter, r *http.Request, params httprouter.Params) {
	if err := h.Store.RemoveInstance(params.ByName("service"), params.ByName("instance_id")); err != nil {
		if IsNotFound(err) {
			jsonError(w, 404, err)
		} else {
			jsonError(w, 500, err)
		}
		return
	}
}

func (h *httpAPI) GetInstances(w http.ResponseWriter, r *http.Request, params httprouter.Params) {
	if strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
		h.handleStream(w, params, discoverd.EventKindUp|discoverd.EventKindUpdate|discoverd.EventKindDown)
		return
	}

	instances := h.Store.Get(params.ByName("service"))
	if instances == nil {
		jsonError(w, 404, errors.New("service not found"))
		return
	}
	httphelper.JSON(w, 200, instances)
}

func (h *httpAPI) GetLeader(w http.ResponseWriter, r *http.Request, params httprouter.Params) {
	if strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
		h.handleStream(w, params, discoverd.EventKindLeader)
		return
	}

	leader := h.Store.GetLeader(params.ByName("service"))
	if leader == nil {
		jsonError(w, 404, errors.New("no leader found"))
		return
	}
	httphelper.JSON(w, 200, leader)
}

func (h *httpAPI) GetServiceStream(w http.ResponseWriter, r *http.Request, params httprouter.Params) {
	if strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
		h.handleStream(w, params, discoverd.EventKindAll)
		return
	}
}

func (h *httpAPI) handleStream(w http.ResponseWriter, params httprouter.Params, kind discoverd.EventKind) {
	sw := sse.NewWriter(w)
	enc := json.NewEncoder(httphelper.FlushWriter{Writer: sw, Enabled: true})
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.WriteHeader(200)
	sw.Flush()

	ch := make(chan *discoverd.Event, 64) // TODO: figure out how big this buffer should be
	done := make(chan struct{})

	go func() {
		defer close(done)
		for e := range ch {
			if err := enc.Encode(e); err != nil {
				return
			}
		}
	}()

	stream := h.Store.Subscribe(params.ByName("service"), true, kind, ch)

	if cn, ok := w.(http.CloseNotifier); ok {
		go func() {
			<-cn.CloseNotify()
			stream.Close()
		}()
	} else {
		defer stream.Close()
	}

	<-done

	if err := stream.Err(); err != nil {
		sw.Error(err)
	}
}
