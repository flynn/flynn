package discoverd

import (
	"encoding/json"
	"errors"
	"reflect"
	"time"

	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/flannel/subnet"
)

// Network is stored in service metadata and contains both the config and list
// of subnet leases.
type Network struct {
	Config  json.RawMessage             `json:"config"`
	Subnets map[string]*json.RawMessage `json:"subnets"`

	index uint64
}

type registry struct {
	client  *discoverd.Client
	service discoverd.Service
	events  chan *discoverd.Event
}

func NewRegistry(client *discoverd.Client, serviceName string) (subnet.Registry, error) {
	service := client.Service(serviceName)
	events := make(chan *discoverd.Event)
	_, err := service.Watch(events)
	if err != nil {
		return nil, err
	}
	return &registry{client: client, service: service, events: events}, nil
}

func (r *registry) GetConfig() ([]byte, error) {
	net, err := r.getNetwork()
	if err != nil {
		return nil, err
	}
	return net.Config, nil
}

func (r *registry) GetSubnets() (*subnet.Response, error) {
	net, err := r.getNetwork()
	if err != nil {
		return nil, err
	}
	return newResponse(net), nil
}

func rawJSON(s string) *json.RawMessage {
	raw := json.RawMessage(s)
	return &raw
}

func (r *registry) CreateSubnet(sn, data string, ttl uint64) (*subnet.Response, error) {
	net, err := r.getNetwork()
	if err != nil {
		return nil, err
	}
	if _, ok := net.Subnets[sn]; ok {
		return nil, subnet.ErrSubnetExists
	}
	if net.Subnets == nil {
		net.Subnets = make(map[string]*json.RawMessage)
	}
	net.Subnets[sn] = rawJSON(data)
	if err := r.setNetwork(net); err != nil {
		return nil, err
	}
	return newResponse(net), nil
}

func (r *registry) UpdateSubnet(sn, data string, ttl uint64) (*subnet.Response, error) {
	net, err := r.getNetwork()
	if err != nil {
		return nil, err
	}
	if net.Subnets == nil {
		net.Subnets = make(map[string]*json.RawMessage)
	}
	net.Subnets[sn] = rawJSON(data)
	if err := r.setNetwork(net); err != nil {
		return nil, err
	}
	return newResponse(net), nil
}

// knownSubnets is updated each time we receive a service metadata event so
// that we can calculate which subnets are new and should be returned from
// WatchSubnets
var knownSubnets map[string]*json.RawMessage

// WatchSubnets waits for a service metadata event with an index greater than
// a given value, and then returns a response.
func (r *registry) WatchSubnets(since uint64, stop chan bool) (*subnet.Response, error) {
	for {
		select {
		case event, ok := <-r.events:
			if !ok {
				return nil, errors.New("unexpected close of discoverd event stream")
			}
			if event.Kind != discoverd.EventKindServiceMeta {
				continue
			}
			net := &Network{}
			if err := json.Unmarshal(event.ServiceMeta.Data, net); err != nil {
				return nil, err
			}
			subnets := make(map[string][]byte)
			for subnet, data := range net.Subnets {
				if known, ok := knownSubnets[subnet]; ok && reflect.DeepEqual(known, data) {
					continue
				}
				subnets[subnet] = []byte(*data)
			}
			knownSubnets = net.Subnets
			if event.ServiceMeta.Index >= since {
				return &subnet.Response{Subnets: subnets, Index: event.ServiceMeta.Index}, nil
			}
		case <-stop:
			return nil, nil
		}
	}
}

func (r *registry) getNetwork() (*Network, error) {
	meta, err := r.service.GetMeta()
	if err != nil {
		return nil, err
	}
	net := &Network{}
	if err := json.Unmarshal(meta.Data, net); err != nil {
		return nil, err
	}
	net.index = meta.Index
	return net, nil
}

func (r *registry) setNetwork(net *Network) error {
	data, err := json.Marshal(net)
	if err != nil {
		return err
	}
	meta := &discoverd.ServiceMeta{Data: data, Index: net.index}
	if err := r.service.SetMeta(meta); err != nil {
		return err
	}
	net.index = meta.Index
	return nil
}

func newResponse(net *Network) *subnet.Response {
	// set a far future expiry as discoverd meta does not currently support ttl.
	exp := time.Now().AddDate(10, 0, 0)
	subnets := make(map[string][]byte, len(net.Subnets))
	for subnet, data := range net.Subnets {
		subnets[subnet] = []byte(*data)
	}
	return &subnet.Response{Subnets: subnets, Index: net.index, Expiration: &exp}
}
