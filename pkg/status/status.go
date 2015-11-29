package status

import (
	"encoding/json"
	"net/http"

	"github.com/flynn/flynn/pkg/version"
)

/*

	GET /.well-known/status
	Accept: application/json

	{
	  "data": {
		"status": "healthy", // ENUM: "health", "unhealthy" (200, 500)
		"version": "ver_str",
		"detail": {
			// ... optional arbitrary service-specific information
		}
	  }
	}

*/

const Path = "/.well-known/status"

type Code string

const (
	CodeHealthy   Code = "healthy"
	CodeUnhealthy Code = "unhealthy"
)

var (
	Healthy   = Status{Status: CodeHealthy}
	Unhealthy = Status{Status: CodeUnhealthy}

	HealthyHandler Handler = func() Status { return Healthy }
)

type Status struct {
	Status  Code             `json:"status"`
	Detail  *json.RawMessage `json:"detail,omitempty"`
	Version string           `json:"version,omitempty"`
}

func New(healthy bool, detail interface{}) (Status, error) {
	var s Status
	s.Status = CodeHealthy
	if !healthy {
		s.Status = CodeUnhealthy
	}
	if detail != nil {
		res, err := json.Marshal(detail)
		if err != nil {
			return Status{}, err
		}
		data := json.RawMessage(res)
		s.Detail = &data
	}
	return s, nil
}

func SimpleHandler(f func() error) Handler {
	return func() Status {
		if err := f(); err != nil {
			return Unhealthy
		}
		return Healthy
	}
}

type Handler func() Status

func (f Handler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	s := f()
	s.Version = version.String()
	if s.Status == CodeHealthy {
		w.WriteHeader(200)
	} else {
		if s.Status == "" {
			s.Status = CodeUnhealthy
		}
		w.WriteHeader(500)
	}

	res, _ := json.MarshalIndent(struct {
		Data Status `json:"data"`
	}{s}, "", "  ")
	w.Write(res)
	w.Write([]byte("\n"))
}

func AddHandler(h Handler) {
	http.Handle(Path, h)
}
