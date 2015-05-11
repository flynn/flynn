package resource

import (
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/docker/docker/pkg/units"
	"github.com/flynn/flynn/pkg/typeconv"
)

type Spec struct {
	// Request, if set, is the amount of resource a job expects to consume,
	// so the job should only be placed on a host with at least this amount
	// of resource available, and once scheduled this amount of resource
	// should then be unavailable on the given host.
	Request *int64 `json:"request"`

	// Limit, if set, is an upper limit on the amount of resource a job can
	// consume, the outcome of hitting this limit being implementation
	// defined (e.g. a system error, throttling, catchable / uncatchable
	// signals etc.)
	Limit *int64 `json:"limit"`
}

type Type string

const (
	// TypeMemory specifies the available memory in bytes inside a container.
	TypeMemory Type = "memory"

	// TypeMaxFD specifies a value one greater than the maximum file
	// descriptor number that can be opened inside a container.
	TypeMaxFD Type = "max_fd"

	// TypeMaxProcs specifies the maximum number of processes which can
	// be started inside a container.
	TypeMaxProcs Type = "max_procs"
)

var defaults = Resources{
	TypeMemory:   {Request: typeconv.Int64Ptr(1 * units.GiB), Limit: typeconv.Int64Ptr(1 * units.GiB)},
	TypeMaxFD:    {Request: typeconv.Int64Ptr(10000), Limit: typeconv.Int64Ptr(10000)},
	TypeMaxProcs: {Request: typeconv.Int64Ptr(256), Limit: typeconv.Int64Ptr(256)},
}

type Resources map[Type]Spec

func Defaults() Resources {
	r := make(Resources)
	SetDefaults(&r)
	return r
}

func SetDefaults(r *Resources) {
	if *r == nil {
		*r = make(Resources, len(defaults))
	}
	for typ, s := range defaults {
		spec := (*r)[typ]
		if spec.Limit == nil {
			spec.Limit = typeconv.Int64Ptr(*s.Limit)
		}
		if spec.Request == nil {
			spec.Request = spec.Limit
		}
		(*r)[typ] = spec
	}
}
