package server

import "github.com/flynn/flynn/discoverd/client"

type Backend interface {
	AddService(service string, config *discoverd.ServiceConfig) error
	RemoveService(service string) error
	AddInstance(service string, inst *discoverd.Instance) error
	RemoveInstance(service, id string) error
	SetServiceMeta(service string, meta *discoverd.ServiceMeta) error
	SetLeader(service, id string) error
	StartSync() error
	Close() error
	Ping() error
}

type SyncHandler interface {
	AddService(service string, config *discoverd.ServiceConfig)
	RemoveService(service string)
	AddInstance(service string, inst *discoverd.Instance)
	RemoveInstance(service, id string)
	SetService(service string, config *discoverd.ServiceConfig, data []*discoverd.Instance)
	SetServiceMeta(service string, meta []byte, index uint64)
	SetLeader(service, id string)
	ListServices() []string
}
