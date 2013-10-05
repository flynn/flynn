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

//TODO Name the arguments in the interface
type DiscoveryBackend interface {
	Subscribe(name string) (chan *ServiceUpdate, error)
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

func (s *DiscoverAgent) Subscribe(args *Args, sendUpdate func(reply interface{}) error) error {
	updates, err := s.Backend.Subscribe(args.Name)
	for update := range updates {
		err = sendUpdate(update)
		if err != nil {
			return err
		}
	}
	return err
}

func (s *DiscoverAgent) Register(args *Args, ret *struct{}) error {
	return s.Backend.Register(args.Name, args.Addr, nil) // TODO: attrs!
}

func (s *DiscoverAgent) Unregister(args *Args, ret *struct{}) error {
	return s.Backend.Unregister(args.Name, args.Addr)
}

func (s *DiscoverAgent) Heartbeat(args *Args, ret *struct{}) error {
	return s.Backend.Heartbeat(args.Name, args.Addr)
}
