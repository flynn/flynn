package server

import "github.com/flynn/flynn/discoverd/client"

type Backend interface {
	AddService(service string) error
	RemoveService(service string) error
	AddInstance(service string, inst *discoverd.Instance) error
	RemoveInstance(service, id string) error
	SetServiceMeta(service string, meta *discoverd.ServiceMeta) error
	StartSync() error
	Close() error
}

type SyncHandler interface {
	AddService(service string)
	RemoveService(service string)
	AddInstance(service string, inst *discoverd.Instance)
	RemoveInstance(service, id string)
	SetService(service string, data []*discoverd.Instance)
	SetServiceMeta(service string, meta []byte, index uint64)
	ListServices() []string
}
