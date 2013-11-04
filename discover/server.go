package discover

import (
	"net/http"
	"log"

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

type Agent struct {
	Backend DiscoveryBackend
	Address string
}

func NewServer(addr string) *Agent {
	return &Agent{
		Backend: &EtcdBackend{Client: etcd.NewClient(nil)},
		Address: addr,
	}
}

func ListenAndServe(server *Agent) error {
	rpcplus.HandleHTTP()
	err := rpcplus.Register(server)
	if err != nil {
		return err
	}
	return http.ListenAndServe(server.Address, nil)
}

func (s *Agent) Subscribe(args *Args, stream rpcplus.Stream) error {
	updates, err := s.Backend.Subscribe(args.Name)
	if err != nil {
		log.Println("Subscribe: ", err)
		stream.Send <- &ServiceUpdate{} // be sure to unblock client
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

func (s *Agent) Register(args *Args, ret *struct{}) error {
	err := s.Backend.Register(args.Name, args.Addr, args.Attrs)
	if err != nil {
		log.Println("Register: ", err)
	}
	return err
}

func (s *Agent) Unregister(args *Args, ret *struct{}) error {
	err := s.Backend.Unregister(args.Name, args.Addr)
	if err != nil {
		log.Println("Unregister: ", err)
	}
	return err
}

func (s *Agent) Heartbeat(args *Args, ret *struct{}) error {
	err := s.Backend.Heartbeat(args.Name, args.Addr)
	if err != nil {
		log.Println("Heartbeat: ", err)
	}
	return err
}
