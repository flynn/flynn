package httphelper

import (
	"log"
	"net/http"
	"time"
)

func NewRequestLogger(log *log.Logger, handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		start := time.Now()
		addr := req.Header.Get("X-Real-IP")
		if addr == "" {
			addr = req.Header.Get("X-Forwarded-For")
			if addr == "" {
				addr = req.RemoteAddr
			}
		}
		log.Printf("Started %s %s for %s", req.Method, req.URL.Path, addr)

		rw := NewResponseWriter(w)
		handler.ServeHTTP(rw, req)

		log.Printf("Completed %v %s in %v\n", rw.Status(), http.StatusText(rw.Status()), time.Since(start))
	})
}
