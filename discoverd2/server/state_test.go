package server

import (
	"fmt"
	"reflect"
	"sort"
	"sync/atomic"
	"testing"
	"time"

	. "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
	"github.com/flynn/flynn/pkg/random"
)

// Hook gocheck up to the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type StateSuite struct{}

var _ = Suite(&StateSuite{})

var instanceIdx uint64

func fakeInstance() *Instance {
	octet := func() int { return random.Math.Intn(255) + 1 }
	return &Instance{
		ID:    random.String(16),
		Addr:  fmt.Sprintf("%d.%d.%d.%d:%d", octet(), octet(), octet(), octet(), random.Math.Intn(65535)+1),
		Proto: "tcp",
		Meta:  map[string]string{"foo": "bar"},
		Index: uint(atomic.AddUint64(&instanceIdx, 1)),
	}
}

func assertHasInstance(c *C, list []*Instance, want ...*Instance) {
	for _, want := range want {
		for _, have := range list {
			if reflect.DeepEqual(have, want) {
				return
			}
		}
		c.Errorf("couldn't find %#v in %#v", want, list)
	}
}

func assertNoEvent(c *C, events chan *Event) {
	select {
	case e := <-events:
		c.Errorf("unexpected event %v %#v", e, e.Instance)
	default:
	}
}

func assertEvent(c *C, events chan *Event, service string, kind EventKind, instance *Instance) {
	var event *Event
	select {
	case event = <-events:
	case <-time.After(10 * time.Second):
		c.Errorf("timed out waiting for %s %#v", kind, instance)
	}
	c.Assert(event, DeepEquals, &Event{
		Service:  service,
		Kind:     kind,
		Instance: instance,
	})
}

func receiveEvents(c *C, events chan *Event, count int) map[string]*Event {
	res := make(map[string]*Event, count)
	for i := 0; i < count; i++ {
		select {
		case e := <-events:
			c.Logf("+ event %s", e)
			res[e.Instance.ID] = e
		case <-time.After(10 * time.Second):
			c.Errorf("expected %d events, got %d", count, len(res))
		}
	}
	assertNoEvent(c, events)
	return res
}

func (StateSuite) TestAddInstance(c *C) {
	state := NewState()
	events := make(chan *Event, 1)
	state.Subscribe("a", false, EventKindUpdate|EventKindUp, events)

	// + with service that doesn't exist
	inst1 := fakeInstance()
	state.AddInstance("a", inst1)
	data := state.Get("a")
	c.Assert(data, HasLen, 1)
	c.Assert(data[0], DeepEquals, inst1)
	assertEvent(c, events, "a", EventKindUp, inst1)

	// + with new instance
	inst2 := fakeInstance()
	state.AddInstance("a", inst2)
	data = state.Get("a")
	c.Assert(data, HasLen, 2)
	assertHasInstance(c, data, inst2)
	assertEvent(c, events, "a", EventKindUp, inst2)

	// + with updated instance
	inst3 := *inst2
	inst3.Meta = map[string]string{"test": "b"}
	state.AddInstance("a", &inst3)
	data = state.Get("a")
	c.Assert(data, HasLen, 2)
	assertHasInstance(c, data, &inst3)
	assertEvent(c, events, "a", EventKindUpdate, &inst3)

	// + with unchanged instance
	inst4 := inst3
	state.AddInstance("a", &inst4)
	c.Assert(data, HasLen, 2)
	assertHasInstance(c, data, &inst4)
	assertNoEvent(c, events)
}

func (StateSuite) TestDeleteInstance(c *C) {
	state := NewState()
	events := make(chan *Event, 1)
	state.Subscribe("a", false, EventKindDown, events)

	// + with service that doesn't exist
	state.RemoveInstance("a", "b")
	assertNoEvent(c, events)

	// + with instance that doesn't exist
	inst := fakeInstance()
	state.AddInstance("a", inst)
	state.RemoveInstance("a", "b")
	c.Assert(state.Get("a"), HasLen, 1)
	assertNoEvent(c, events)

	// + with instance that exists
	state.RemoveInstance("a", inst.ID)
	c.Assert(state.Get("a"), HasLen, 0)
	assertEvent(c, events, "a", EventKindDown, inst)
}

func (StateSuite) TestSetService(c *C) {
	state := NewState()
	events := make(chan *Event, 3)
	state.Subscribe("a", false, EventKindAll, events)

	// + with service that doesn't exist
	newData := []*Instance{fakeInstance(), fakeInstance()}
	state.SetService("a", newData)
	data := state.Get("a")
	c.Assert(data, HasLen, 2)
	assertHasInstance(c, data, newData...)
	for _, expected := range newData {
		assertEvent(c, events, "a", EventKindUp, expected)
	}
	assertNoEvent(c, events)

	// + with service that exists and zero-length new
	state.SetService("a", nil)
	c.Assert(state.Get("a"), HasLen, 0)
	// make sure we get exactly two down events, one for each existing instance
	down := receiveEvents(c, events, 2)
	for _, e := range down {
		c.Assert(e.Kind, Equals, EventKindDown)
		c.Assert(e.Service, Equals, "a")
	}
	c.Assert(down[newData[0].ID].Instance, DeepEquals, newData[0])
	c.Assert(down[newData[1].ID].Instance, DeepEquals, newData[1])

	// + one existing, one updated, one new, one deleted
	initial := []*Instance{fakeInstance(), fakeInstance(), fakeInstance()}
	state.SetService("a", initial)
	// eat the three up events
	receiveEvents(c, events, 3)

	existing := initial[0]
	deleted := initial[1]
	modified := *initial[2]
	modified.Meta = map[string]string{"a": "b"}
	added := fakeInstance()

	state.SetService("a", []*Instance{existing, &modified, added})
	data = state.Get("a")
	c.Assert(data, HasLen, 3)
	assertHasInstance(c, data, existing, &modified, added)

	changes := receiveEvents(c, events, 3)

	modifiedEvent := changes[modified.ID]
	c.Assert(modifiedEvent.Kind, Equals, EventKindUpdate)
	c.Assert(modifiedEvent.Service, Equals, "a")
	c.Assert(modifiedEvent.Instance, DeepEquals, &modified)

	deletedEvent := changes[deleted.ID]
	c.Assert(deletedEvent.Kind, Equals, EventKindDown)
	c.Assert(deletedEvent.Service, Equals, "a")
	c.Assert(deletedEvent.Instance, DeepEquals, deleted)

	addedEvent := changes[added.ID]
	c.Assert(addedEvent.Kind, Equals, EventKindUp)
	c.Assert(addedEvent.Service, Equals, "a")
	c.Assert(addedEvent.Instance, DeepEquals, added)
}

func (StateSuite) TestGetNilService(c *C) {
	state := NewState()
	c.Assert(state.Get("a"), HasLen, 0)
}

func (StateSuite) TestSubscribe(c *C) {
	state := NewState()

	inst1 := fakeInstance()
	state.AddInstance("a", inst1)

	events := make(chan *Event, 1)
	stream := state.Subscribe("a", true, EventKindAll, events)

	// initial instance
	assertEvent(c, events, "a", EventKindUp, inst1)

	inst2 := fakeInstance()
	state.AddInstance("a", inst2)

	// subsequent event
	assertEvent(c, events, "a", EventKindUp, inst2)

	stream.Close()
	_, open := <-events
	c.Assert(open, Equals, false)

	// create another update to confirm nothing is blocked
	state.AddInstance("a", fakeInstance())
}

func (StateSuite) TestBlockedSubscription(c *C) {
	state := NewState()
	events := make(chan *Event)
	stream := state.Subscribe("a", true, EventKindUp, events)

	// send to the channel will fail immediately because there is no receiver
	state.AddInstance("a", fakeInstance())

	_, open := <-events
	c.Assert(open, Equals, false)
	c.Assert(stream.Err(), Equals, ErrSendBlocked)
}

func (StateSuite) TestListServices(c *C) {
	state := NewState()
	state.AddInstance("a", fakeInstance())
	state.AddInstance("b", fakeInstance())
	services := state.ListServices()
	sort.Strings(services)
	c.Assert(services, DeepEquals, []string{"a", "b"})
}

func (StateSuite) TestInstanceValid(c *C) {
	for _, t := range []struct {
		name string
		inst *Instance
		err  string
	}{
		{
			name: "invalid proto",
			inst: &Instance{
				ID:    md5sum("TCP-127.0.0.1:2"),
				Proto: "TCP",
				Addr:  "127.0.0.1:2",
			},
			err: ErrInvalidProto.Error(),
		},
		{
			name: "empty proto",
			inst: &Instance{
				ID:   md5sum("-127.0.0.1:2"),
				Addr: "127.0.0.1:2",
			},
			err: ErrUnsetProto.Error(),
		},
		{
			name: "invalid addr",
			inst: &Instance{
				ID:    md5sum("tcp-asdf"),
				Proto: "tcp",
				Addr:  "asdf",
			},
			err: "missing port in address asdf",
		},
		{
			name: "empty addr",
			inst: &Instance{
				ID:    md5sum("tcp-"),
				Proto: "tcp",
				Addr:  "",
			},
			err: "missing port in address",
		},
		{
			name: "empty id",
			inst: &Instance{
				Proto: "tcp",
				Addr:  "127.0.0.1:2",
			},
			err: "discoverd: instance id is incorrect, expected 35ee81ee2b44f7521139b75e865e3c98",
		},
		{
			name: "invalid id",
			inst: &Instance{
				ID:    "asdf",
				Proto: "tcp",
				Addr:  "127.0.0.1:2",
			},
			err: "discoverd: instance id is incorrect, expected 35ee81ee2b44f7521139b75e865e3c98",
		},
		{
			name: "valid",
			inst: &Instance{
				ID:    md5sum("tcp-127.0.0.1:2"),
				Proto: "tcp",
				Addr:  "127.0.0.1:2",
			},
		},
	} {
		c.Log(t.name)
		err := t.inst.Valid()
		if t.err == "" {
			c.Check(err, IsNil)
		} else {
			c.Assert(err, NotNil)
			c.Check(err.Error(), Equals, t.err)
		}
	}
}
