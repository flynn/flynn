package redis

import (
	"net/http"

	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/status"
	"github.com/julienschmidt/httprouter"
	"github.com/inconshreveable/log15"
)

// Handler represents an HTTP handler for the redis process.
type Handler struct {
	router *httprouter.Router

	Process     *Process
	Heartbeater discoverd.Heartbeater
	Logger      log15.Logger
}

// NewHandler returns a new instance of Handler.
func NewHandler() *Handler {
	h := &Handler{
		router: httprouter.New(),
	}
	h.router.Handler("GET", status.Path, status.Handler(h.healthStatus))
	h.router.GET("/status", h.handleGetStatus)
	h.router.POST("/stop", h.handlePostStop)
	h.router.POST("/restore", h.handlePostRestore)
	return h
}

// ServeHTTP serves an HTTP request and returns a response.
func (h *Handler) ServeHTTP(w http.ResponseWriter, req *http.Request) { h.router.ServeHTTP(w, req) }

// healthStatus returns whether the process is healthy or unhealthy.
func (h *Handler) healthStatus() status.Status {
	info, err := h.Process.Info()
	if err != nil || !info.Running {
		return status.Unhealthy
	}
	return status.Healthy
}

func (h *Handler) handleGetStatus(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	info, err := h.Process.Info()
	if err != nil {
		// Log the error, but don't return a 500. We will always have some
		// information to return, but redis may not be online.
		h.Logger.Error("error getting redis info", "err", err)
	}
	httphelper.JSON(w, 200, &Status{Process: info})
}

func (h *Handler) handlePostStop(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	if err := h.Heartbeater.Close(); err != nil {
		httphelper.Error(w, err)
		return
	}
	w.WriteHeader(200)
}

func (h *Handler) handlePostRestore(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	if err := h.Process.Restore(req.Body); err != nil {
		httphelper.Error(w, err)
		return
	}
	w.WriteHeader(200)
}
