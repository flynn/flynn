package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/julienschmidt/httprouter"
	"github.com/flynn/flynn/discoverd/client"
	hh "github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/sse"
	"github.com/flynn/flynn/pkg/status"
	"github.com/flynn/flynn/pkg/stream"
)

type Datastore interface {
	// Typically implemented by a Backend
	AddService(service string, config *discoverd.ServiceConfig) error
	RemoveService(service string) error
	AddInstance(service string, inst *discoverd.Instance) error
	RemoveInstance(service, id string) error
	SetServiceMeta(service string, meta *discoverd.ServiceMeta) error
	SetLeader(service, id string) error
	Ping() error

	// Typically implemented by State
	Get(service string) []*discoverd.Instance
	GetConfig(service string) *discoverd.ServiceConfig
	GetServiceMeta(service string) *discoverd.ServiceMeta
	GetLeader(service string) *discoverd.Instance
	Subscribe(service string, sendCurrent bool, kinds discoverd.EventKind, ch chan *discoverd.Event) stream.Stream
}

type basicDatastore struct {
	*State
	Backend
}

func (d basicDatastore) Ping() error {
	return d.Backend.Ping()
}

func (d basicDatastore) AddService(service string, config *discoverd.ServiceConfig) error {
	return d.Backend.AddService(service, config)
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

func (d basicDatastore) SetServiceMeta(service string, meta *discoverd.ServiceMeta) error {
	return d.Backend.SetServiceMeta(service, meta)
}

func (d basicDatastore) SetLeader(service, id string) error {
	return d.Backend.SetLeader(service, id)
}

func NewBasicDatastore(state *State, backend Backend) Datastore {
	return &basicDatastore{state, backend}
}

func NewHTTPHandler(ds Datastore) http.Handler {
	router := httprouter.New()

	api := &httpAPI{
		Store: ds,
	}

	router.Handler("GET", status.Path, status.SimpleHandler(ds.Ping))

	router.PUT("/services/:service", api.AddService)
	router.DELETE("/services/:service", api.RemoveService)
	router.GET("/services/:service", api.GetServiceStream)

	router.PUT("/services/:service/meta", api.SetServiceMeta)
	router.GET("/services/:service/meta", api.GetServiceMeta)

	router.PUT("/services/:service/instances/:instance_id", api.AddInstance)
	router.DELETE("/services/:service/instances/:instance_id", api.RemoveInstance)
	router.GET("/services/:service/instances", api.GetInstances)

	router.PUT("/services/:service/leader", api.SetLeader)
	router.GET("/services/:service/leader", api.GetLeader)

	router.GET("/ping", func(http.ResponseWriter, *http.Request, httprouter.Params) {})

	return router
}

type httpAPI struct {
	Store Datastore
}

func (h *httpAPI) AddService(w http.ResponseWriter, r *http.Request, params httprouter.Params) {
	service := params.ByName("service")
	if err := ValidServiceName(service); err != nil {
		hh.ValidationError(w, "", err.Error())
		return
	}

	config := &discoverd.ServiceConfig{}
	if err := hh.DecodeJSON(r, config); err != nil {
		hh.Error(w, err)
		return
	}

	if err := h.Store.AddService(service, config); err != nil {
		if IsServiceExists(err) {
			hh.ObjectExistsError(w, err.Error())
		} else {
			hh.Error(w, err)
		}
		return
	}
}

func (h *httpAPI) RemoveService(w http.ResponseWriter, r *http.Request, params httprouter.Params) {
	service := params.ByName("service")
	if err := ValidServiceName(service); err != nil {
		hh.ValidationError(w, "", err.Error())
		return
	}
	if err := h.Store.RemoveService(params.ByName("service")); err != nil {
		if IsNotFound(err) {
			hh.ObjectNotFoundError(w, err.Error())
		} else {
			hh.Error(w, err)
		}
		return
	}
}

func (h *httpAPI) SetServiceMeta(w http.ResponseWriter, r *http.Request, params httprouter.Params) {
	meta := &discoverd.ServiceMeta{}
	if err := hh.DecodeJSON(r, meta); err != nil {
		hh.Error(w, err)
		return
	}

	if err := h.Store.SetServiceMeta(params.ByName("service"), meta); err != nil {
		if IsNotFound(err) {
			hh.ObjectNotFoundError(w, err.Error())
		} else {
			hh.Error(w, err)
		}
		return
	}

	hh.JSON(w, 200, meta)
}

func (h *httpAPI) GetServiceMeta(w http.ResponseWriter, r *http.Request, params httprouter.Params) {
	meta := h.Store.GetServiceMeta(params.ByName("service"))
	if meta == nil {
		hh.ObjectNotFoundError(w, "service meta not found")
		return
	}
	hh.JSON(w, 200, meta)
}

func (h *httpAPI) AddInstance(w http.ResponseWriter, r *http.Request, params httprouter.Params) {
	inst := &discoverd.Instance{}
	if err := json.NewDecoder(r.Body).Decode(inst); err != nil {
		hh.Error(w, err)
		return
	}
	if err := inst.Valid(); err != nil {
		hh.ValidationError(w, "", err.Error())
		return
	}
	if err := h.Store.AddInstance(params.ByName("service"), inst); err != nil {
		if IsNotFound(err) {
			hh.ObjectNotFoundError(w, err.Error())
		} else {
			hh.Error(w, err)
		}
		return
	}
}

func (h *httpAPI) RemoveInstance(w http.ResponseWriter, r *http.Request, params httprouter.Params) {
	if err := h.Store.RemoveInstance(params.ByName("service"), params.ByName("instance_id")); err != nil {
		if IsNotFound(err) {
			hh.ObjectNotFoundError(w, err.Error())
		} else {
			hh.Error(w, err)
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
		hh.ObjectNotFoundError(w, "service not found")
		return
	}
	hh.JSON(w, 200, instances)
}

func (h *httpAPI) SetLeader(w http.ResponseWriter, r *http.Request, params httprouter.Params) {
	service := params.ByName("service")
	config := h.Store.GetConfig(service)
	if config == nil || config.LeaderType != discoverd.LeaderTypeManual {
		hh.ValidationError(w, "", "service leader election type is not manual")
		return
	}

	inst := &discoverd.Instance{}
	if err := hh.DecodeJSON(r, inst); err != nil {
		hh.Error(w, err)
		return
	}

	if err := h.Store.SetLeader(service, inst.ID); err != nil {
		hh.Error(w, err)
		return
	}
}

func (h *httpAPI) GetLeader(w http.ResponseWriter, r *http.Request, params httprouter.Params) {
	if strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
		h.handleStream(w, params, discoverd.EventKindLeader)
		return
	}

	leader := h.Store.GetLeader(params.ByName("service"))
	if leader == nil {
		hh.ObjectNotFoundError(w, "no leader found")
		return
	}
	hh.JSON(w, 200, leader)
}

func (h *httpAPI) GetServiceStream(w http.ResponseWriter, r *http.Request, params httprouter.Params) {
	if strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
		h.handleStream(w, params, discoverd.EventKindAll)
		return
	}
}

func (h *httpAPI) handleStream(w http.ResponseWriter, params httprouter.Params, kind discoverd.EventKind) {
	ch := make(chan *discoverd.Event, 64) // TODO: figure out how big this buffer should be
	stream := h.Store.Subscribe(params.ByName("service"), true, kind, ch)
	s := sse.NewStream(w, ch, nil)
	s.Serve()
	s.Wait()
	stream.Close()
	if err := stream.Err(); err != nil {
		s.CloseWithError(err)
	}
}
