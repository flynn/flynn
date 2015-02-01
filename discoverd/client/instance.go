package discoverd

import (
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"sync"
)

type EventKind uint

const (
	EventKindUp EventKind = 1 << iota
	EventKindUpdate
	EventKindDown
	EventKindLeader
	EventKindCurrent
	EventKindServiceMeta
	EventKindAll     = ^EventKind(0)
	EventKindUnknown = EventKind(0)
)

var eventKindStrings = map[EventKind]string{
	EventKindUp:          "up",
	EventKindUpdate:      "update",
	EventKindDown:        "down",
	EventKindLeader:      "leader",
	EventKindCurrent:     "current",
	EventKindUnknown:     "unknown",
	EventKindServiceMeta: "service_meta",
}

func (k EventKind) String() string {
	if s, ok := eventKindStrings[k]; ok {
		return s
	}
	return eventKindStrings[EventKindUnknown]
}

func (k EventKind) Any(kinds ...EventKind) bool {
	for _, other := range kinds {
		if k&other != 0 {
			return true
		}
	}
	return false
}

var eventKindMarshalJSON = make(map[EventKind][]byte, len(eventKindStrings))
var eventKindUnmarshalJSON = make(map[string]EventKind, len(eventKindStrings))

func init() {
	for k, s := range eventKindStrings {
		json := `"` + s + `"`
		eventKindMarshalJSON[k] = []byte(json)
		eventKindUnmarshalJSON[json] = k
	}
}

func (k EventKind) MarshalJSON() ([]byte, error) {
	data, ok := eventKindMarshalJSON[k]
	if ok {
		return data, nil
	}
	return eventKindMarshalJSON[EventKindUnknown], nil
}

func (k *EventKind) UnmarshalJSON(data []byte) error {
	if kind, ok := eventKindUnmarshalJSON[string(data)]; ok {
		*k = kind
	}
	return nil
}

type Event struct {
	Service     string       `json:"service"`
	Kind        EventKind    `json:"kind"`
	Instance    *Instance    `json:"instance,omitempty"`
	ServiceMeta *ServiceMeta `json:"service_meta,omitempty"`
}

func (e *Event) String() string {
	return fmt.Sprintf("[%s] %s %#v", e.Service, e.Kind, e.Instance)
}

// Instance is a single running instance of a service. It is immutable after it
// has been initialized.
type Instance struct {
	// ID is unique within the service, and is currently defined as
	// Hex(MD5(Proto + "-" + Addr)) but this may change in the future.
	ID string `json:"id"`

	// Addr is the IP/port address that can be used to communicate with the
	// service. It must be valid to dial this address.
	Addr string `json:"addr"`

	// Proto is the protocol used to connect to the service, examples include:
	// tcp, udp, http, https. It must be lowercase alphanumeric.
	Proto string `json:"proto"`

	// Meta is arbitrary metadata specified when registering the instance.
	Meta map[string]string `json:"meta,omitempty"`

	// Index is the logical epoch of the initial registration of the instance.
	// It is guaranteed to be unique, greater than zero, not change as long as
	// the instance does not expire, and sort with other indexes in the order of
	// instance creation.
	Index uint64 `json:"index,omitempty"`

	// addrOnce is used to initialize host/port
	addrOnce sync.Once
	host     string
	port     string
}

func (inst *Instance) Equal(other *Instance) bool {
	return inst.Addr == other.Addr &&
		inst.Proto == other.Proto &&
		mapEqual(inst.Meta, other.Meta)
}

func (inst *Instance) Valid() error {
	if err := inst.validProto(); err != nil {
		return err
	}
	if _, _, err := net.SplitHostPort(inst.Addr); err != nil {
		return err
	}
	if expected := inst.id(); inst.ID != expected {
		return fmt.Errorf("discoverd: instance id is incorrect, expected %s", expected)
	}
	return nil
}

func (inst *Instance) Host() string {
	inst.splitHostPort()
	return inst.host
}

func (inst *Instance) Port() string {
	inst.splitHostPort()
	return inst.port
}

func (inst *Instance) splitHostPort() {
	inst.addrOnce.Do(func() {
		inst.host, inst.port, _ = net.SplitHostPort(inst.Addr)
	})
}

var ErrUnsetProto = errors.New("discoverd: proto must be set")
var ErrInvalidProto = errors.New("discoverd: proto must be lowercase alphanumeric")

func (inst *Instance) validProto() error {
	if inst.Proto == "" {
		return ErrUnsetProto
	}
	for _, r := range inst.Proto {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return ErrInvalidProto
		}
	}
	return nil
}

func (inst *Instance) id() string {
	return md5sum(inst.Proto + "-" + inst.Addr)
}

func (inst *Instance) Clone() *Instance {
	res := *inst
	res.Meta = make(map[string]string, len(inst.Meta))
	for k, v := range inst.Meta {
		res.Meta[k] = v
	}
	return &res
}

func md5sum(data string) string {
	digest := md5.Sum([]byte(data))
	return hex.EncodeToString(digest[:])
}

func mapEqual(x, y map[string]string) bool {
	if len(x) != len(y) {
		return false
	}
	for k, v := range x {
		if yv, ok := y[k]; !ok || yv != v {
			return false
		}
	}
	return true
}
