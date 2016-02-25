package main

import (
	"net/http"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/julienschmidt/httprouter"
	"github.com/flynn/flynn/Godeps/_workspace/src/gopkg.in/inconshreveable/log15.v2"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/sirenia/client"
	"github.com/flynn/flynn/pkg/sirenia/state"
	"github.com/flynn/flynn/pkg/status"
)

func ServeHTTP(pg *Postgres, peer *state.Peer, hb discoverd.Heartbeater, log log15.Logger) error {
	api := &HTTP{
		pg:   pg,
		peer: peer,
		hb:   hb,
		log:  log,
	}
	r := httprouter.New()
	r.Handler("GET", status.Path, status.Handler(api.GetHealthStatus))
	r.GET("/status", api.GetStatus)
	r.POST("/stop", api.Stop)
	return http.ListenAndServe(":5433", r)
}

type HTTP struct {
	pg   *Postgres
	peer *state.Peer
	hb   discoverd.Heartbeater
	log  log15.Logger
}

func (h *HTTP) GetHealthStatus() status.Status {
	info := h.peer.Info()
	if info.State == nil || info.RetryPending != nil ||
		(info.Role != state.RolePrimary && info.Role != state.RoleSync && info.Role != state.RoleAsync) {
		return status.Unhealthy
	}
	pg, err := h.pg.Info()
	if err != nil || !pg.Running || !pg.UserExists {
		return status.Unhealthy
	}
	if info.Role == state.RolePrimary {
		if !pg.ReadWrite {
			return status.Unhealthy
		}
		if !info.State.Singleton && (pg.SyncedDownstream == nil || info.State.Sync == nil || info.State.Sync.ID != pg.SyncedDownstream.ID) {
			return status.Unhealthy
		}
	}

	return status.Healthy
}

func (h *HTTP) GetStatus(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	res := &client.Status{
		Peer: h.peer.Info(),
	}
	var err error
	res.Database, err = h.pg.Info()
	if err != nil {
		// Log the error, but don't return a 500. We will always have some
		// information to return, but postgres may not be online.
		h.log.Error("error getting postgres info", "err", err)
	}
	httphelper.JSON(w, 200, res)
}

func (h *HTTP) Stop(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	if err := h.hb.Close(); err != nil {
		httphelper.Error(w, err)
		return
	}
	if err := h.peer.Stop(); err != nil {
		httphelper.Error(w, err)
		return
	}
	w.WriteHeader(200)
}
