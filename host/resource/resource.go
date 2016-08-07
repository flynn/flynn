package resource

import (
	"fmt"
	"strconv"

	"github.com/docker/go-units"
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

	// TypeCPU specifies the amount of milliCPU requested. A milliCPU is
	// conceptually 1/1000 of a CPU core (eg 500m is half of a CPU core). In
	// practice, a 1000 milliCPU limit is equivalent to 1024 CPU shares.
	TypeCPU Type = "cpu"

	TypeDisk = "disk"

	// TypeMaxFD specifies a value one greater than the maximum file
	// descriptor number that can be opened inside a container.
	TypeMaxFD Type = "max_fd"

	// TypeMaxProcs specifies the maximum number of processes which can
	// be started inside a container.
	TypeMaxProcs Type = "max_procs"
)

var defaults = Resources{
	TypeMemory: {Request: typeconv.Int64Ptr(1 * units.GiB), Limit: typeconv.Int64Ptr(1 * units.GiB)},
	TypeCPU:    {Limit: typeconv.Int64Ptr(1000)}, // results in Linux default of 1024 shares
	TypeDisk:   {Request: typeconv.Int64Ptr(100 * units.MiB), Limit: typeconv.Int64Ptr(100 * units.MiB)},
	TypeMaxFD:  {Request: typeconv.Int64Ptr(10000), Limit: typeconv.Int64Ptr(10000)},
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

func ToType(s string) (Type, bool) {
	for typ := range defaults {
		if string(typ) == s {
			return typ, true
		}
	}
	return Type(""), false
}

func ParseLimit(typ Type, s string) (int64, error) {
	switch typ {
	case TypeMemory, TypeDisk:
		return units.RAMInBytes(s)
	default:
		return units.FromHumanSize(s)
	}
}

func FormatLimit(typ Type, limit int64) string {
	switch typ {
	case TypeMemory, TypeDisk:
		return byteSize(limit)
	default:
		return strconv.FormatInt(limit, 10)
	}
}

var byteUnits = []string{"B", "kB", "MB", "GB", "TB", "PB", "EB", "ZB", "YB"}

func byteSize(limit int64) string {
	i := 0
	unit := 1024.0
	size := float64(limit)
	for size >= unit {
		size = size / unit
		i++
	}
	return fmt.Sprintf("%.4g%s", size, byteUnits[i])
}
