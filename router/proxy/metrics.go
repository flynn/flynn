package proxy

import (
	"net/http"
	"strconv"
	"time"

	router "github.com/flynn/flynn/router/types"
	"github.com/prometheus/client_golang/prometheus"
)

func init() {
	prometheus.MustRegister(backendHTTPResponseMetric)
	prometheus.MustRegister(backendHTTPConnErrorMetric)
	prometheus.MustRegister(backendHTTPLatencyMetric)
}

var backendHTTPConnErrorMetric = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "router_backend_http_conn_errors_total",
		Help: "Router backend HTTP connection errors",
	},
	[]string{
		"backend_app",
		"backend_service",
		"backend_job_id",
	},
)

func trackBackendHTTPConnError(backend *router.Backend) {
	backendHTTPConnErrorMetric.With(prometheus.Labels{
		"backend_app":     backend.App,
		"backend_service": backend.Service,
		"backend_job_id":  backend.JobID,
	}).Inc()
}

var backendHTTPResponseMetric = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "router_backend_http_responses_total",
		Help: "Router backend HTTP responses",
	},
	[]string{
		"backend_app",
		"backend_service",
		"backend_job_id",
		"http_status",
	},
)

func trackBackendHTTPResponse(backend *router.Backend, res *http.Response) {
	backendHTTPResponseMetric.With(prometheus.Labels{
		"backend_app":     backend.App,
		"backend_service": backend.Service,
		"backend_job_id":  backend.JobID,
		"http_status":     strconv.Itoa(res.StatusCode),
	}).Inc()
}

var backendHTTPLatencyMetric = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "router_backend_http_latency_seconds",
		Help: "Router backend HTTP latency",
	},
	[]string{
		"backend_app",
		"backend_service",
		"backend_job_id",
		"http_status",
	},
)

func trackBackendHTTPLatency(backend *router.Backend, res *http.Response, duration time.Duration) {
	backendHTTPLatencyMetric.With(prometheus.Labels{
		"backend_app":     backend.App,
		"backend_service": backend.Service,
		"backend_job_id":  backend.JobID,
		"http_status":     strconv.Itoa(res.StatusCode),
	}).Add(float64(duration) / float64(time.Second))
}
