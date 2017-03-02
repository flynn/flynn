package discoverd

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/flynn/flynn/pkg/attempt"
	"github.com/flynn/flynn/pkg/stream"
)

type WatchState string

const (
	WatchStateConnected    WatchState = "connected"
	WatchStateDisconnected WatchState = "disconnected"
)

// Watch is a wrapper around an event stream which reconnects on error and
// generates any events which were missed by comparing local state to the
// current state returned by the server.
type Watch struct {
	instances   map[string]*Instance
	leader      *Instance
	serviceMeta *ServiceMeta
	err         error
	done        chan struct{}
	doneOnce    sync.Once
	stateCh     atomic.Value // chan WatchState
}

func NewWatch() *Watch {
	return &Watch{
		instances: make(map[string]*Instance),
		done:      make(chan struct{}),
	}
}

// SetStateChannel sets a channel which will receive events when the watch
// connects to / disconnects from the server.
func (w *Watch) SetStateChannel(ch chan WatchState) {
	w.stateCh.Store(ch)
}

func (w *Watch) maybeSendState(state WatchState) {
	if ch := w.stateCh.Load(); ch != nil {
		ch.(chan WatchState) <- state
	}
}

func (w *Watch) Err() error {
	return w.err
}

func (w *Watch) Close() error {
	w.doneOnce.Do(func() { close(w.done) })
	return nil
}

// addInst adds the given instance to the local state and returns whether the
// instance already existed in the local state, and if it did exist, whether it
// was identical.
func (w *Watch) addInst(inst *Instance) (known, identical bool) {
	var i *Instance
	i, known = w.instances[inst.ID]
	identical = known && i.Equal(inst)
	w.instances[inst.ID] = inst
	return
}

// delInst removes the given instance from the local state.
func (w *Watch) delInst(inst *Instance) {
	delete(w.instances, inst.ID)
}

var connectAttempts = attempt.Strategy{
	Total: 30 * time.Second,
	Delay: 200 * time.Millisecond,
}

// Watch starts an event stream and sends events to the given channel.
//
// It reconnects to the server on error and generates any events which were
// missed by comparing local state to the current state returned by the server.
//
// On reconnect, current events from the server are handled as follows:
//
// * EventKindUp
//     Compared to local instances, and only sent if the instance was previously
//     unknown or has changed in some way (with the event's kind being set to
//     EventKindUpdate in the latter case).
//
// * EventKindLeader
//     Compared to the most recently known leader, and only sent if it differs.
//
// * EventKindServiceMeta
//     Compared to the most recently known service metadata, and only sent if
//     it differs.
func (s *service) Watch(ch chan *Event) (stream.Stream, error) {
	var events chan *Event
	var stream stream.Stream
	watch := NewWatch()
	connect := func() (err error) {
		events = make(chan *Event)
		stream, err = s.client.Stream("GET", fmt.Sprintf("/services/%s", s.name), nil, events)
		if err != nil {
			return err
		}
		watch.maybeSendState(WatchStateConnected)
		return nil
	}
	if err := connect(); err != nil {
		close(ch)
		return nil, err
	}
	go func() {
		defer stream.Close()
		defer close(ch)
		isCurrent := false

		// current stores the instances returned between connecting to
		// the server and the server returning EventKindCurrent, and is
		// used to generate missing events.
		current := make(map[string]*Instance)

		for {
			select {
			case <-watch.done:
				return
			case event, ok := <-events:
				if !ok {
					isCurrent = false
					current = make(map[string]*Instance)
					watch.maybeSendState(WatchStateDisconnected)
					if err := connectAttempts.Run(connect); err != nil {
						watch.err = err
						return
					}
					continue
				}
				switch event.Kind {
				case EventKindCurrent:
					// send down events for any instances which are not current.
					for id, inst := range watch.instances {
						if _, ok := current[id]; !ok {
							ch <- &Event{
								Service:  s.name,
								Kind:     EventKindDown,
								Instance: inst,
							}
							watch.delInst(inst)
						}
					}
					isCurrent = true
				case EventKindUp:
					if !isCurrent {
						current[event.Instance.ID] = event.Instance
					}
					known, identical := watch.addInst(event.Instance)

					// if we are current, or we haven't seen the instance before, send the event
					if isCurrent || !known {
						break
					}

					// don't send identical up events
					if identical {
						continue
					}

					// the instance is known but has changed from when we last saw it, so send
					// EventKindUpdate instead
					event.Kind = EventKindUpdate
				case EventKindUpdate:
					watch.addInst(event.Instance)
				case EventKindDown:
					watch.delInst(event.Instance)
				case EventKindLeader:
					// don't send duplicate leader events
					if !isCurrent && watch.leader != nil && watch.leader.ID == event.Instance.ID {
						continue
					}
					watch.leader = event.Instance
				case EventKindServiceMeta:
					// don't send duplicate service meta events
					if !isCurrent && watch.serviceMeta != nil && watch.serviceMeta.Index == event.ServiceMeta.Index {
						continue
					}
					watch.serviceMeta = event.ServiceMeta
				}
				select {
				case ch <- event:
				case <-watch.done:
					return
				}
			}
		}
	}()
	return watch, nil
}
