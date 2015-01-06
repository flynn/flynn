package server

type Backend interface {
	AddInstance(service string, inst *Instance) error
	RemoveInstance(service, id string) error
	StartSync() error
	Close() error
}

type SyncHandler interface {
	AddInstance(service string, inst *Instance)
	RemoveInstance(service, id string)
	SetService(service string, data []*Instance)
	ListServices() []string
}
