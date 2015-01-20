package httphelper

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
)

func NewResponseWriter(w http.ResponseWriter) *ResponseWriter {
	return &ResponseWriter{w: w}
}

type ResponseWriter struct {
	w      http.ResponseWriter
	status int
}

func (r *ResponseWriter) Status() int {
	return r.status
}

func (r *ResponseWriter) WriteHeader(s int) {
	r.w.WriteHeader(s)
	r.status = s
}

func (r *ResponseWriter) Header() http.Header {
	return r.w.Header()
}

func (r *ResponseWriter) Write(b []byte) (int, error) {
	if !r.Written() {
		r.WriteHeader(http.StatusOK)
	}
	return r.w.Write(b)
}

func (r *ResponseWriter) Written() bool {
	return r.status != 0
}

func (r *ResponseWriter) CloseNotify() <-chan bool {
	return r.w.(http.CloseNotifier).CloseNotify()
}

func (r *ResponseWriter) Flush() {
	flusher, ok := r.w.(http.Flusher)
	if ok {
		flusher.Flush()
	}
}

func (r *ResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := r.w.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("the ResponseWriter doesn't support the Hijacker interface")
	}
	return hijacker.Hijack()
}
