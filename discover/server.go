package discover

import (
	"github.com/coreos/go-etcd/etcd"
	"github.com/flynn/rpcplus"
	"log"
	"net"
	"net/http"
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

type DiscoveryBackend interface {
	Subscribe(string) (chan *ServiceUpdate, error)
	Register(string, string, map[string]string) error
	Unregister(string, string) error
	Heartbeat(string, string) error
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

func ServeForever(server *DiscoverAgent) {
	rpcplus.Register(server)
	rpcplus.HandleHTTP()
	l, e := net.Listen("tcp", server.Address)
	if e != nil {
		log.Fatal("listen error:", e)
	}
	http.Serve(l, nil)
}

func (s *DiscoverAgent) Subscribe(args *Args, sendUpdate func(reply interface{}) error) error {
	updates, _ := s.Backend.Subscribe(args.Name)
	for update := range updates {
		err := sendUpdate(update)
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *DiscoverAgent) Register(args *Args, success *bool) error {
	err := s.Backend.Register(args.Name, args.Addr, map[string]string{}) // TODO: attrs!
	if err != nil {
		*success = false
	} else {
		*success = true
	}
	return nil
}

func (s *DiscoverAgent) Unregister(args *Args, success *bool) error {
	err := s.Backend.Unregister(args.Name, args.Addr)
	if err != nil {
		*success = false
	} else {
		*success = true
	}
	return nil
}

func (s *DiscoverAgent) Heartbeat(args *Args, success *bool) error {
	err := s.Backend.Heartbeat(args.Name, args.Addr)
	if err != nil {
		*success = false
	} else {
		*success = true
	}
	return nil
}
