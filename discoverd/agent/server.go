package agent

import (
	"errors"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/coreos/go-etcd/etcd"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/kavu/go_reuseport"
	"github.com/flynn/flynn/pkg/attempt"
	"github.com/flynn/flynn/pkg/rpcplus"
	rpc "github.com/flynn/flynn/pkg/rpcplus/comborpc"
)

const (
	// HeartbeatIntervalSecs is the expected interval at which services register themselves.
	HeartbeatIntervalSecs = 5
	// MissedHearbeatTTL allows for services to miss a heartbeat before being set to offline.
	MissedHearbeatTTL = 5
)

// ServiceUpdate is sent when a service comes online or goes offline.
type ServiceUpdate struct {
	Name    string
	Addr    string
	Online  bool
	Attrs   map[string]string
	Created uint
}

// Args represents the data sent to discoverd's register and unregister API methods.
type Args struct {
	Name  string
	Addr  string
	Attrs map[string]string
}

// UpdateStream represents a subscription to changes in service registration.
type UpdateStream interface {
	Chan() chan *ServiceUpdate
	Close()
}

// DiscoveryBackend represents a system that registers/unregisters services and notifies on updates.
type DiscoveryBackend interface {
	Subscribe(name string) (UpdateStream, error)
	Register(name string, addr string, attrs map[string]string) error
	Unregister(name string, addr string) error
}

// Agent represents the discoverd server--the backend its using, where it's listening, etc.
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

// NewServer creates a new discoverd server listening at addr and backed by etcd.
func NewServer(addr string, etcdAddrs []string) *Agent {
	client := etcd.NewClient(etcdAddrs)

	// check to make sure that etcd is online and accepting connections
	// etcd takes a while to come online, so we attempt a GET multiple times
	err := Attempts.Run(func() (err error) {
		_, err = client.Get("/", false, false)
		if e, ok := err.(*etcd.EtcdError); ok && e.ErrorCode == 100 {
			// Valid 404 from etcd (> v0.5)
			err = nil
		}
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

// ListenAndServe starts the the discoverd agent.
func ListenAndServe(server *Agent) error {
	rpc.HandleHTTP()
	if err := rpc.Register(server); err != nil {
		return err
	}
	listener, err := reuseport.NewReusablePortListener("tcp4", server.Address)
	if err != nil {
		log.Fatal(err)
	}
	return http.Serve(listener, nil)
}

var externalIP = os.Getenv("EXTERNAL_IP")

func expandAddr(addr string) string {
	if addr[0] == ':' {
		return externalIP + addr
	}
	return addr
}

// Subscribe returns a stream of ServiceUpdate objects for the given service name.
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

// Register announces a service is online at an address.
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

// Unregister announces a service has gone offline.
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
