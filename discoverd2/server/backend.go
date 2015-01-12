package server

import "github.com/flynn/flynn/discoverd2/client"

type Backend interface {
	AddService(service string) error
	RemoveService(service string) error
	AddInstance(service string, inst *discoverd.Instance) error
	RemoveInstance(service, id string) error
	StartSync() error
	Close() error
}

type SyncHandler interface {
	AddService(service string)
	RemoveService(service string)
	AddInstance(service string, inst *discoverd.Instance)
	RemoveInstance(service, id string)
	SetService(service string, data []*discoverd.Instance)
	ListServices() []string
}
