package discoverd

import (
	"time"
)

var DefaultClient = NewClient()

func NewService(name string) Service {
	return DefaultClient.Service(name)
}

func GetInstances(service string, timeout time.Duration) ([]*Instance, error) {
	return DefaultClient.Instances(service, timeout)
}

func AddServiceAndRegister(service, addr string) (Heartbeater, error) {
	return DefaultClient.AddServiceAndRegister(service, addr)
}

func Register(service, addr string) (Heartbeater, error) {
	return DefaultClient.Register(service, addr)
}
