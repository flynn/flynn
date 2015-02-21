package main

import (
	"errors"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"strings"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/julienschmidt/httprouter"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/host/volume/api"
	"github.com/flynn/flynn/host/volume/manager"
	"github.com/flynn/flynn/pinkerton"
	"github.com/flynn/flynn/pinkerton/layer"
	"github.com/flynn/flynn/pkg/cluster"
	"github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/sse"
)

type Host struct {
	state   *State
	backend Backend
}

func (h *Host) StopJob(id string) error {
	job := h.state.GetJob(id)
	if job == nil {
		return errors.New("host: unknown job")
	}
	switch job.Status {
	case host.StatusStarting:
		h.state.SetForceStop(id)
		return nil
	case host.StatusRunning:
		return h.backend.Stop(id)
	default:
		return errors.New("host: job is already stopped")
	}
}

func (h *Host) streamEvents(id string, w http.ResponseWriter) error {
	ch := h.state.AddListener(id)
	defer h.state.RemoveListener(id, ch)
	sse.ServeStream(w, ch, nil)
	return nil
}

type jobAPI struct {
	host *Host
}

func (h *jobAPI) ListJobs(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	if strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
		if err := h.host.streamEvents("all", w); err != nil {
			httphelper.Error(w, err)
		}
		return
	}
	res := h.host.state.Get()

	httphelper.JSON(w, 200, res)
}

func (h *jobAPI) GetJob(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	id := ps.ByName("id")

	if strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
		if err := h.host.streamEvents(id, w); err != nil {
			httphelper.Error(w, err)
		}
		return
	}
	job := h.host.state.GetJob(id)
	httphelper.JSON(w, 200, job)
}

func (h *jobAPI) StopJob(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	id := ps.ByName("id")
	if err := h.host.StopJob(id); err != nil {
		httphelper.Error(w, err)
		return
	}
	w.WriteHeader(200)
}

func (h *jobAPI) PullImages(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	tufDB, err := extractTufDB(r)
	if err != nil {
		httphelper.Error(w, err)
	}
	defer os.Remove(tufDB)

	info := make(chan layer.PullInfo)
	stream := sse.NewStream(w, info, nil)
	go stream.Serve()

	if err := pinkerton.PullImages(
		tufDB,
		r.URL.Query().Get("repository"),
		r.URL.Query().Get("driver"),
		r.URL.Query().Get("root"),
		info,
	); err != nil {
		stream.CloseWithError(err)
		return
	}

	stream.Wait()
}

func extractTufDB(r *http.Request) (string, error) {
	defer r.Body.Close()
	tmp, err := ioutil.TempFile("", "tuf-db")
	if err != nil {
		return "", err
	}
	defer tmp.Close()
	if _, err := io.Copy(tmp, r.Body); err != nil {
		return "", err
	}
	return tmp.Name(), nil
}

func (h *jobAPI) RegisterRoutes(r *httprouter.Router) error {
	r.GET("/host/jobs", h.ListJobs)
	r.GET("/host/jobs/:id", h.GetJob)
	r.DELETE("/host/jobs/:id", h.StopJob)
	r.POST("/host/pull-images", h.PullImages)
	return nil
}

func serveHTTP(host *Host, attach *attachHandler, clus *cluster.Client, vman *volumemanager.Manager) (*httprouter.Router, error) {
	l, err := net.Listen("tcp", ":1113")
	if err != nil {
		return nil, err
	}

	r := httprouter.New()

	r.POST("/attach", attach.ServeHTTP)

	jobAPI := &jobAPI{host}
	jobAPI.RegisterRoutes(r)
	volAPI := volumeapi.NewHTTPAPI(clus, vman)
	volAPI.RegisterRoutes(r)

	go http.Serve(l, httphelper.ContextInjector("host", httphelper.NewRequestLogger(r)))

	return r, nil
}
