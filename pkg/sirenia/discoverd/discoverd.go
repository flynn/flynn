package discoverd

import (
	"encoding/json"
	"sort"
	"sync"
	"time"

	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/sirenia/state"
	"github.com/inconshreveable/log15"
)

type Discoverd struct {
	service discoverd.Service
	events  chan *state.DiscoverdEvent
	runOnce sync.Once
	log     log15.Logger
}

func NewDiscoverd(s discoverd.Service, log log15.Logger) state.Discoverd {
	return &Discoverd{
		service: s,
		events:  make(chan *state.DiscoverdEvent),
		log:     log,
	}
}

func (d *Discoverd) SetState(state *state.DiscoverdState) error {
	data, err := json.Marshal(state.State)
	if err != nil {
		return err
	}
	meta := &discoverd.ServiceMeta{Index: state.Index, Data: data}
	if state.State.Primary != nil {
		meta.LeaderID = state.State.Primary.ID
	}
	if err := d.service.SetMeta(meta); err != nil {
		return err
	}
	state.Index = meta.Index
	return nil
}

func (d *Discoverd) receiveEvents() {
	log := d.log.New("fn", "receiveEvents")
	log.Info("starting event handler")

	var peers instSlice
	addPeer := func(add *discoverd.Instance) {
		for i, inst := range peers {
			if inst.ID == add.ID {
				peers[i] = add
				return
			}
		}
		peers = append(peers, add)
		sort.Sort(peers)
	}
	delPeer := func(del *discoverd.Instance) {
		for i, inst := range peers {
			if inst.ID == del.ID {
				peers = append(peers[:i], peers[i+1:]...)
				return
			}
		}
	}

	dstate := &state.DiscoverdState{}
	initialized := false
	var sentIndex uint64
	var sentPeers instSlice
	maybeEvent := func(current bool) {
		if !current {
			return
		}
		if !initialized {
			sentPeers = make(instSlice, len(peers))
			copy(sentPeers, peers)
			sentIndex = dstate.Index
			initialized = true
			d.events <- &state.DiscoverdEvent{
				Kind:  state.DiscoverdEventInit,
				Peers: sentPeers,
				State: dstate,
			}
			return
		}

		if dstate.Index > sentIndex {
			sentIndex = dstate.Index
			d.events <- &state.DiscoverdEvent{
				Kind:  state.DiscoverdEventState,
				State: dstate,
			}
		}
		if !sentPeers.Equal(peers) {
			sentPeers = make(instSlice, len(peers))
			copy(sentPeers, peers)
			d.events <- &state.DiscoverdEvent{
				Kind:  state.DiscoverdEventPeers,
				Peers: sentPeers,
			}
		}
	}

	backoff := false
	for {
		current := false
		events := make(chan *discoverd.Event)
		stream, err := d.service.Watch(events)
		if err != nil {
			log.Error("error watching service", "at", "watch", "err", err)
			if backoff {
				time.Sleep(time.Second)
			}
			backoff = true
			continue
		}
		backoff = false

		for e := range events {
			switch e.Kind {
			case discoverd.EventKindUp:
				addPeer(e.Instance)
				maybeEvent(current)
			case discoverd.EventKindServiceMeta:
				if e.ServiceMeta.Index <= dstate.Index {
					continue
				}
				dstate = &state.DiscoverdState{
					Index: e.ServiceMeta.Index,
					State: &state.State{},
				}
				if len(e.ServiceMeta.Data) > 0 {
					if err := json.Unmarshal(e.ServiceMeta.Data, dstate.State); err != nil {
						log.Error("error unmarshalling service meta into state", "at", "unmarshal_state", "err", err)
					}
				}
				maybeEvent(current)
			case discoverd.EventKindCurrent:
				current = true
				maybeEvent(current)
			case discoverd.EventKindUpdate:
				addPeer(e.Instance)
			case discoverd.EventKindDown:
				delPeer(e.Instance)
				maybeEvent(current)
			}
		}
		log.Error("disconnected from service watch", "at", "watch_disconnect", "err", stream.Err())
		if backoff {
			time.Sleep(time.Second)
		}
		backoff = true
	}
}

func (d *Discoverd) Events() <-chan *state.DiscoverdEvent {
	d.runOnce.Do(func() { go d.receiveEvents() })
	return d.events
}

type instSlice []*discoverd.Instance

func (p instSlice) Len() int           { return len(p) }
func (p instSlice) Less(i, j int) bool { return p[i].Index < p[j].Index }
func (p instSlice) Swap(i, j int)      { p[i], p[j] = p[j], p[i] }

func (x instSlice) Equal(y instSlice) bool {
	if len(x) != len(y) {
		return false
	}
	for i, xx := range x {
		if y[i].ID != xx.ID {
			return false
		}
	}
	return true
}
