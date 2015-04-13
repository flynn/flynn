package reqlog

import (
	"net/http"
	"sync/atomic"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/golang.org/x/net/context"
	"github.com/flynn/flynn/Godeps/_workspace/src/gopkg.in/inconshreveable/log15.v2"
)

type Log struct {
	ID         string
	Method     string
	Path       string
	Host       string
	RemoteAddr string
	Backend    string
	Start      time.Time
	Finish     time.Time
	StatusCode int
	BodyBytes  uint64

	// heroku-isms:

	Level string
	Code  string
}

func (rl *Log) WriteTo(logger log15.Logger) {
	logger.Info("request",
		"method", rl.Method,
		"path", rl.Path,
		"host", rl.Host,
		"fwd", rl.RemoteAddr,
		"backend", rl.Backend,
		"service", durationMS(rl.Start, rl.Finish),
		"status", rl.StatusCode,
		"bytes.body", rl.BodyBytes,
		"request_id", rl.ID,
	)
}

type ctxKey int

const (
	ctxKeyLog ctxKey = iota
)

// NewContext creates a new context that carries the provided request log.
func NewContext(ctx context.Context, log Log) context.Context {
	return context.WithValue(ctx, ctxKeyLog, &log)
}

// StartTimeFromContext extracts a start time from a context.
func FromContext(ctx context.Context) (log *Log, ok bool) {
	log, ok = ctx.Value(ctxKeyLog).(*Log)
	return
}

// TODO: maybe add a FromRequest function

type ResponseWriterPlus interface {
	http.CloseNotifier
	http.Flusher
	http.Hijacker
	http.ResponseWriter
}

// ResponseTracker is wrapper of http.ResponseWriter that keeps track of its HTTP
// status code and body size.
type ResponseTracker struct {
	ResponseWriterPlus

	StatusCode int
	// TODO(bgentry): for now, this only counts the bytes in the body. There's no
	// easy way to count the bytes in the response headers, unfortunately:
	// https://groups.google.com/forum/#!topic/golang-nuts/zq_i3Hf7Nbs
	Size uint64
}

func (rt *ResponseTracker) Write(b []byte) (int, error) {
	if rt.StatusCode == 0 {
		// The status will be StatusOK if WriteHeader has not been called yet
		rt.StatusCode = http.StatusOK
	}
	size, err := rt.ResponseWriterPlus.Write(b)
	atomic.AddUint64(&rt.Size, uint64(size))
	return size, err
}

func (rt *ResponseTracker) WriteHeader(s int) {
	rt.ResponseWriterPlus.WriteHeader(s)
	rt.StatusCode = s
}

func durationMS(start, finish time.Time) int {
	if finish.IsZero() || start.IsZero() {
		return 0
	}
	return int(finish.Sub(start) / time.Millisecond)
}
