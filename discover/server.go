package discover

import (
	"net"
	"net/http"

	"github.com/coreos/go-etcd/etcd"
	"github.com/flynn/rpcplus"
)

const (
	HeartbeatIntervalSecs = 5
	MissedHearbeatTTL     = 5
)

type ServiceUpdate struct {
	Name   string
	Addr   string
	Online bool
	Attrs  map[string]string
}

type Args struct {
	Name  string
	Addr  string
	Attrs map[string]string
}

type UpdateStream interface {
	Chan() chan *ServiceUpdate
	Close()
}

type DiscoveryBackend interface {
	Subscribe(name string) (UpdateStream, error)
	Register(name string, addr string, attrs map[string]string) error
	Unregister(name string, addr string) error
	Heartbeat(name string, addr string) error
}

type DiscoverAgent struct {
	Backend DiscoveryBackend
	Address string
}

func NewServer() *DiscoverAgent {
	return &DiscoverAgent{
		Backend: &EtcdBackend{Client: etcd.NewClient()},
		Address: ":1111",
	}
}

func ListenAndServe(server *DiscoverAgent) error {
	err := rpcplus.Register(server)
	if err != nil {
		return err
	}
	rpcplus.HandleHTTP()
	l, err := net.Listen("tcp", server.Address)
	http.Serve(l, nil)
	return err
}

func (s *DiscoverAgent) Subscribe(args *Args, stream rpcplus.Stream) error {
	updates, err := s.Backend.Subscribe(args.Name)
	if err != nil {
		return err
	}
	for update := range updates.Chan() {
		select {
		case stream.Send <- update:
		case <-stream.Error:
			updates.Close()
			return nil
		}
	}
	return nil
}

func (s *DiscoverAgent) Register(args *Args, ret *struct{}) error {
	return s.Backend.Register(args.Name, args.Addr, args.Attrs)
}

func (s *DiscoverAgent) Unregister(args *Args, ret *struct{}) error {
	return s.Backend.Unregister(args.Name, args.Addr)
}

func (s *DiscoverAgent) Heartbeat(args *Args, ret *struct{}) error {
	return s.Backend.Heartbeat(args.Name, args.Addr)
}
