package resource

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/docker/go-units"
	"github.com/flynn/flynn/pkg/typeconv"
)

const DefaultTempDiskSize int64 = 100 * units.MiB

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

	// TypeTempDisk specifies the available disk space in bytes of the
	// temporary root disk of the container.
	TypeTempDisk = "temp_disk"

	// TypeMaxFD specifies a value one greater than the maximum file
	// descriptor number that can be opened inside a container.
	TypeMaxFD Type = "max_fd"

	// TypeMaxProcs specifies the maximum number of processes which can
	// be started inside a container.
	TypeMaxProcs Type = "max_procs"
)

var defaults = Resources{
	TypeMemory:   {Request: typeconv.Int64Ptr(1 * units.GiB), Limit: typeconv.Int64Ptr(1 * units.GiB)},
	TypeCPU:      {Limit: typeconv.Int64Ptr(1000)}, // results in Linux default of 1024 shares
	TypeTempDisk: {Request: typeconv.Int64Ptr(DefaultTempDiskSize), Limit: typeconv.Int64Ptr(DefaultTempDiskSize)},
	TypeMaxFD:    {Request: typeconv.Int64Ptr(10000), Limit: typeconv.Int64Ptr(10000)},
}

type Resources map[Type]Spec

func (r Resources) SetLimit(typ Type, size int64) {
	r[typ] = Spec{Request: typeconv.Int64Ptr(size), Limit: typeconv.Int64Ptr(size)}
}

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

func ParseCSV(limits string) (Resources, error) {
	return Parse(strings.Split(limits, ","))
}

func Parse(limits []string) (Resources, error) {
	resources := make(Resources, len(limits))
	for _, limit := range limits {
		typVal := strings.SplitN(limit, "=", 2)
		if len(typVal) != 2 {
			return nil, fmt.Errorf("invalid resource limit: %q", limit)
		}
		typ, ok := ToType(typVal[0])
		if !ok {
			return nil, fmt.Errorf("invalid resource limit type: %q", typVal)
		}
		val, err := ParseLimit(typ, typVal[1])
		if err != nil {
			return nil, fmt.Errorf("invalid resource limit value: %q", typVal[1])
		}
		resources[typ] = Spec{Limit: typeconv.Int64Ptr(val)}
	}
	return resources, nil
}

func ParseLimit(typ Type, s string) (int64, error) {
	switch typ {
	case TypeMemory, TypeTempDisk:
		return units.RAMInBytes(s)
	default:
		return units.FromHumanSize(s)
	}
}

func FormatLimit(typ Type, limit int64) string {
	switch typ {
	case TypeMemory, TypeTempDisk:
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
