package httphelper

import (
	"net/http"
	"time"

	log "github.com/flynn/flynn/Godeps/_workspace/src/gopkg.in/inconshreveable/log15.v2"
	"github.com/flynn/flynn/pkg/random"
)

func NewRequestLogger(ctx log.Ctx, handler http.Handler) http.Handler {
	l := log.New(ctx)
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		start := time.Now()
		addr := req.Header.Get("X-Real-IP")
		if addr == "" {
			addr = req.Header.Get("X-Forwarded-For")
			if addr == "" {
				addr = req.RemoteAddr
			}
		}
		reqID := req.Header.Get("X-Request-ID")
		if reqID == "" {
			reqID = random.UUID()
		}
		logger := l.New(log.Ctx{"req_id": reqID})
		logger.Info("request started", "method", req.Method, "path", req.URL.Path, "addr", addr)

		rw := NewResponseWriter(w)
		handler.ServeHTTP(rw, req)

		logger.Info("request completed", "status", rw.Status(), "duration", time.Since(start))
	})
}
