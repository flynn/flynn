package mariadb

import (
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/sirenia/client"
	"github.com/flynn/flynn/pkg/sirenia/state"
	"github.com/flynn/flynn/pkg/status"
	"github.com/julienschmidt/httprouter"
	"gopkg.in/inconshreveable/log15.v2"
)

// Handler represents an HTTP API handler for the process.
type Handler struct {
	router *httprouter.Router

	Process     *Process
	Peer        *state.Peer
	Heartbeater discoverd.Heartbeater
	Logger      log15.Logger
}

// NewHandler returns a new instance of Handler.
func NewHandler() *Handler {
	h := &Handler{
		router: httprouter.New(),
		Logger: log15.New(),
	}
	h.router.Handler("GET", status.Path, status.Handler(h.healthStatus))
	h.router.GET("/status", h.handleGetStatus)
	h.router.GET("/tunables", h.handleGetTunables)
	h.router.POST("/tunables", h.handleUpdateTunables)
	h.router.GET("/backup", h.handleGetBackup)
	h.router.POST("/stop", h.handlePostStop)
	return h
}

// ServeHTTP serves an HTTP request.
func (h *Handler) ServeHTTP(w http.ResponseWriter, req *http.Request) { h.router.ServeHTTP(w, req) }

// healthStatus returns the current status of the peer and process.
func (h *Handler) healthStatus() status.Status {
	info := h.Peer.Info()
	if info.State == nil || info.RetryPending != nil ||
		(info.Role != state.RolePrimary && info.Role != state.RoleSync && info.Role != state.RoleAsync) {
		return status.Unhealthy
	}

	process, err := h.Process.Info()
	if err != nil || !process.Running || !process.UserExists {
		return status.Unhealthy
	} else if info.Role == state.RolePrimary {
		if !process.ReadWrite {
			return status.Unhealthy
		}
		if !info.State.Singleton && (process.SyncedDownstream == nil || info.State.Sync == nil || info.State.Sync.ID != process.SyncedDownstream.ID) {
			return status.Unhealthy
		}
	}

	return status.Healthy
}

// handleGetStatus handles request to GET /status.
func (h *Handler) handleGetStatus(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	logger := h.Logger.New("fn", "handleGetStatus")

	var status client.Status
	if h.Peer != nil {
		status.Peer = h.Peer.Info()
	}

	info, err := h.Process.Info()
	if err != nil {
		// Log the error, but don't return a 500. We will always have some
		// information to return, but MariaDB may not be online.
		logger.Error("error getting process info", "err", err)
	}
	status.Database = info

	httphelper.JSON(w, 200, &status)
}

func (h *Handler) handleGetTunables(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	info := h.Peer.Info()
	if info.State != nil {
		httphelper.JSON(w, 200, info.State.Tunables)
		return
	}
	httphelper.Error(w, fmt.Errorf("peer has no state"))
}

func (h *Handler) handleUpdateTunables(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	var newTunables state.Tunables
	if err := json.NewDecoder(req.Body).Decode(&newTunables); err != nil {
		httphelper.Error(w, err)
		return
	}
	if err := h.Process.ValidateTunables(newTunables); err != nil {
		httphelper.Error(w, err)
		return
	}
	if err := h.Peer.UpdateTunables(newTunables); err != nil {
		httphelper.Error(w, err)
		return
	}
	httphelper.JSON(w, 200, newTunables)
	return
}

// handleGetBackup handles request to GET /backup.
func (h *Handler) handleGetBackup(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	r, err := h.Process.Backup()
	if err != nil {
		httphelper.Error(w, err)
		return
	}
	defer r.Close()

	w.Header().Set("Trailer", backupChecksumTrailer)
	w.WriteHeader(200)

	hash := sha512.New()
	if _, err := io.Copy(io.MultiWriter(w, hash), r); err != nil {
		h.Logger.Error("error streaming backup", "err", err)
		r.Close()
		return
	}

	// Close the reader and confirm that it finished.
	// Sets a trailer at the end of the HTTP response.
	if err := r.Close(); err == nil {
		// Temporary hack for writing trailers until Flynn uses Go 1.5.
		w.(http.Flusher).Flush()
		conn, buf, _ := w.(http.Hijacker).Hijack()
		buf.WriteString("0\r\n") // eof
		http.Header{backupChecksumTrailer: {hex.EncodeToString(hash.Sum(nil))}}.Write(buf)
		buf.WriteString("\r\n") // end of trailers
		buf.Flush()
		conn.Close()
	} else {
		h.Logger.Error("error returned from backup", "err", err)
	}
}

// handlePostStop handles request to POST /stop.
func (h *Handler) handlePostStop(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	if err := h.Peer.Stop(); err != nil {
		httphelper.Error(w, err)
		return
	}
	if err := h.Heartbeater.Close(); err != nil {
		httphelper.Error(w, err)
		return
	}
	w.WriteHeader(200)
}

const backupChecksumTrailer = "Flynn-Backup-Checksum"
