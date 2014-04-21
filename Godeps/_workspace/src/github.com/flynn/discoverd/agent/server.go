package agent

import (
	"errors"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/coreos/go-etcd/etcd"
	"github.com/flynn/rpcplus"
	"github.com/flynn/go-flynn/attempt"
	rpc "github.com/flynn/rpcplus/comborpc"
)

const (
	HeartbeatIntervalSecs = 5
	MissedHearbeatTTL     = 5
)

type ServiceUpdate struct {
	Name    string
	Addr    string
	Online  bool
	Attrs   map[string]string
	Created uint
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
}

type Agent struct {
	Backend DiscoveryBackend
	Address string
}

// Attempts is the attempt strategy that is used to connect to etcd.
var Attempts = attempt.Strategy{
	Min:   5,
	Total: 5 * time.Second,
	Delay: 200 * time.Millisecond,
}

func NewServer(addr string, etcdAddrs []string) *Agent {
	client := etcd.NewClient(etcdAddrs)

	// check to make sure that etcd is online and accepting connections
	// etcd takes a while to come online, so we attempt a GET multiple times
	err := Attempts.Run(func() (err error) {
		_, err = client.Get("/", false, false)
		return
	})
	if err != nil {
		log.Fatalf("Failed to connect to etcd at %v: %q", etcdAddrs, err)
	}

	return &Agent{
		Backend: &EtcdBackend{Client: client},
		Address: addr,
	}
}

func ListenAndServe(server *Agent) error {
	rpc.HandleHTTP()
	if err := rpc.Register(server); err != nil {
		return err
	}
	return http.ListenAndServe(server.Address, nil)
}

var externalIP = os.Getenv("EXTERNAL_IP")

func expandAddr(addr string) string {
	if addr[0] == ':' {
		return externalIP + addr
	}
	return addr
}

func (s *Agent) Subscribe(args *Args, stream rpcplus.Stream) error {
	updates, err := s.Backend.Subscribe(args.Name)
	if err != nil {
		log.Println("Subscribe: error:", err)
		stream.Send <- &ServiceUpdate{} // be sure to unblock client
		return err
	}
	log.Println("Subscribe:", args.Name)
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

func (s *Agent) Register(args *Args, ret *string) error {
	if len(args.Addr) == 0 {
		return errors.New("discoverd: Addr must be set")
	}
	addr := expandAddr(args.Addr)
	if addr[0] == ':' {
		return errors.New("discoverd: Addr must have address or EXTERNAL_IP must be set")
	}

	err := s.Backend.Register(args.Name, addr, args.Attrs)
	if err != nil {
		log.Println("Register: error:", err)
		return err
	}
	*ret = addr
	log.Println("Register:", args.Name, addr, args.Attrs)
	return nil
}

func (s *Agent) Unregister(args *Args, ret *struct{}) error {
	addr := expandAddr(args.Addr)
	err := s.Backend.Unregister(args.Name, addr)
	if err != nil {
		log.Println("Unregister: error:", err)
		return err
	}
	log.Println("Unregister:", args.Name, addr)
	return nil
}
