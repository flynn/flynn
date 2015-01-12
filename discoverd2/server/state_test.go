package server

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"testing"
	"time"

	. "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
	"github.com/flynn/flynn/discoverd2/client"
	"github.com/flynn/flynn/pkg/random"
)

// Hook gocheck up to the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type StateSuite struct{}

var _ = Suite(&StateSuite{})

func fakeInstance() *discoverd.Instance {
	octet := func() int { return random.Math.Intn(255) + 1 }
	return &discoverd.Instance{
		ID:    random.String(16),
		Addr:  fmt.Sprintf("%d.%d.%d.%d:%d", octet(), octet(), octet(), octet(), random.Math.Intn(65535)+1),
		Proto: "tcp",
		Meta:  map[string]string{"foo": "bar"},
	}
}

func assertHasInstance(c *C, list []*discoverd.Instance, want ...*discoverd.Instance) {
	for _, want := range want {
		for _, have := range list {
			if reflect.DeepEqual(have, want) {
				return
			}
		}
		c.Fatalf("couldn't find %#v in %#v", want, list)
	}
}

func assertNoEvent(c *C, events chan *discoverd.Event) {
	select {
	case e, ok := <-events:
		if !ok {
			c.Fatal("channel closed")
		}
		c.Fatalf("unexpected event %v %#v", e, e.Instance)
	default:
	}
}

func assertEvent(c *C, events chan *discoverd.Event, service string, kind discoverd.EventKind, instance *discoverd.Instance) {
	var event *discoverd.Event
	var ok bool
	select {
	case event, ok = <-events:
		if !ok {
			c.Fatal("channel closed")
		}
	case <-time.After(10 * time.Second):
		c.Fatalf("timed out waiting for %s %#v", kind, instance)
	}

	assertEventEqual(c, event, &discoverd.Event{
		Service:  service,
		Kind:     kind,
		Instance: instance,
	})
}

func assertEventEqual(c *C, actual, expected *discoverd.Event) {
	c.Assert(actual.Service, Equals, expected.Service)
	c.Assert(actual.Kind, Equals, expected.Kind)
	if expected.Instance == nil {
		c.Assert(actual.Instance, IsNil)
		return
	}
	c.Assert(actual.Instance, NotNil)

	// zero out the index for comparison purposes
	eInst := *expected.Instance
	eInst.Index = 0
	aInst := *actual.Instance
	aInst.Index = 0
	c.Assert(aInst, DeepEquals, eInst)
}

func receiveEvents(c *C, events chan *discoverd.Event, count int) map[string][]*discoverd.Event {
	res := receiveSomeEvents(c, events, count)
	assertNoEvent(c, events)
	return res
}

func receiveSomeEvents(c *C, events chan *discoverd.Event, count int) map[string][]*discoverd.Event {
	res := make(map[string][]*discoverd.Event, count)
	for i := 0; i < count; i++ {
		select {
		case e := <-events:
			c.Logf("+ event %s", e)
			res[e.Instance.ID] = append(res[e.Instance.ID], e)
		case <-time.After(10 * time.Second):
			c.Fatalf("expected %d events, got %d", count, len(res))
		}
	}
	return res
}

func (StateSuite) TestAddInstance(c *C) {
	state := NewState()
	events := make(chan *discoverd.Event, 1)
	state.Subscribe("a", false, discoverd.EventKindUpdate|discoverd.EventKindUp, events)

	// + with service that doesn't exist
	inst1 := fakeInstance()
	state.AddInstance("a", inst1)
	data := state.Get("a")
	c.Assert(data, HasLen, 1)
	c.Assert(data[0], DeepEquals, inst1)
	assertEvent(c, events, "a", discoverd.EventKindUp, inst1)

	// + with new instance
	inst2 := fakeInstance()
	state.AddInstance("a", inst2)
	data = state.Get("a")
	c.Assert(data, HasLen, 2)
	assertHasInstance(c, data, inst2)
	assertEvent(c, events, "a", discoverd.EventKindUp, inst2)

	// + with updated instance
	inst3 := *inst2
	inst3.Meta = map[string]string{"test": "b"}
	state.AddInstance("a", &inst3)
	data = state.Get("a")
	c.Assert(data, HasLen, 2)
	assertHasInstance(c, data, &inst3)
	assertEvent(c, events, "a", discoverd.EventKindUpdate, &inst3)

	// + with unchanged instance
	inst4 := inst3
	state.AddInstance("a", &inst4)
	c.Assert(data, HasLen, 2)
	assertHasInstance(c, data, &inst4)
	assertNoEvent(c, events)
}

func (StateSuite) TestDeleteInstance(c *C) {
	state := NewState()
	events := make(chan *discoverd.Event, 1)
	state.Subscribe("a", false, discoverd.EventKindDown, events)

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
	assertEvent(c, events, "a", discoverd.EventKindDown, inst)
}

func (StateSuite) TestSetService(c *C) {
	state := NewState()
	events := make(chan *discoverd.Event, 3)
	state.Subscribe("a", false, discoverd.EventKindUp|discoverd.EventKindUpdate|discoverd.EventKindDown, events)

	// + with service that doesn't exist
	newData := []*discoverd.Instance{fakeInstance(), fakeInstance()}
	state.SetService("a", newData)
	data := state.Get("a")
	c.Assert(data, HasLen, 2)
	assertHasInstance(c, data, newData...)
	for _, expected := range newData {
		assertEvent(c, events, "a", discoverd.EventKindUp, expected)
	}
	assertNoEvent(c, events)

	// + with service that exists and nil new
	state.SetService("a", nil)
	c.Assert(state.Get("a"), IsNil)
	// make sure we get exactly two down events, one for each existing instance
	down := receiveEvents(c, events, 2)
	for _, e := range down {
		c.Assert(e[0].Kind, Equals, discoverd.EventKindDown)
		c.Assert(e[0].Service, Equals, "a")
	}
	c.Assert(down[newData[0].ID][0].Instance, DeepEquals, newData[0])
	c.Assert(down[newData[1].ID][0].Instance, DeepEquals, newData[1])

	// + with service that doesn't exist and zero-length new
	state.SetService("a", []*discoverd.Instance{})
	c.Assert(state.Get("a"), NotNil)
	c.Assert(state.Get("a"), HasLen, 0)

	// + one existing, one updated, one new, one deleted
	initial := []*discoverd.Instance{fakeInstance(), fakeInstance(), fakeInstance()}
	state.SetService("a", initial)
	// eat the three up events
	receiveEvents(c, events, 3)

	existing := initial[0]
	deleted := initial[1]
	modified := *initial[2]
	modified.Meta = map[string]string{"a": "b"}
	added := fakeInstance()

	state.SetService("a", []*discoverd.Instance{existing, &modified, added})
	data = state.Get("a")
	c.Assert(data, HasLen, 3)
	assertHasInstance(c, data, existing, &modified, added)

	changes := receiveEvents(c, events, 3)

	modifiedEvent := changes[modified.ID][0]
	c.Assert(modifiedEvent.Kind, Equals, discoverd.EventKindUpdate)
	c.Assert(modifiedEvent.Service, Equals, "a")
	c.Assert(modifiedEvent.Instance, DeepEquals, &modified)

	deletedEvent := changes[deleted.ID][0]
	c.Assert(deletedEvent.Kind, Equals, discoverd.EventKindDown)
	c.Assert(deletedEvent.Service, Equals, "a")
	c.Assert(deletedEvent.Instance, DeepEquals, deleted)

	addedEvent := changes[added.ID][0]
	c.Assert(addedEvent.Kind, Equals, discoverd.EventKindUp)
	c.Assert(addedEvent.Service, Equals, "a")
	c.Assert(addedEvent.Instance, DeepEquals, added)
}

func (StateSuite) TestLeaderElection(c *C) {
	state := NewState()
	events := make(chan *discoverd.Event, 1)
	state.Subscribe("a", false, discoverd.EventKindLeader, events)

	// nil for non-existent service
	c.Assert(state.GetLeader("a"), IsNil)
	state.AddService("a")
	// nil for existent service with no instances
	c.Assert(state.GetLeader("a"), IsNil)

	// first instance becomes leader
	first := fakeInstance()
	first.Index = 3
	state.AddInstance("a", first)
	assertEvent(c, events, "a", discoverd.EventKindLeader, first)
	c.Assert(state.GetLeader("a"), DeepEquals, first)

	// update doesn't trigger event
	updated := *first
	updated.Meta = map[string]string{"a": "b"}
	state.AddInstance("a", &updated)
	assertNoEvent(c, events)
	c.Assert(state.GetLeader("a"), DeepEquals, &updated)

	// subsequent instance with higher index doesn't become leader
	second := fakeInstance()
	second.Index = 4
	state.AddInstance("a", second)
	assertNoEvent(c, events)
	c.Assert(state.GetLeader("a"), DeepEquals, &updated)

	// subsequent instance with lower index becomes leader
	third := fakeInstance()
	third.Index = 2
	state.AddInstance("a", third)
	assertEvent(c, events, "a", discoverd.EventKindLeader, third)
	c.Assert(state.GetLeader("a"), DeepEquals, third)

	// set with same instance and another instance triggers no events
	fourth := fakeInstance()
	fourth.Index = 5
	state.SetService("a", []*discoverd.Instance{fourth, third})
	assertNoEvent(c, events)
	c.Assert(state.GetLeader("a"), DeepEquals, third)

	// set with same instance and lower index selects a new leader
	fifth := fakeInstance()
	fifth.Index = 1
	state.SetService("a", []*discoverd.Instance{third, fifth})
	assertEvent(c, events, "a", discoverd.EventKindLeader, fifth)
	c.Assert(state.GetLeader("a"), DeepEquals, fifth)

	// set with new instances chooses lowest
	sixth := fakeInstance()
	sixth.Index = 6
	seventh := fakeInstance()
	seventh.Index = 7
	state.SetService("a", []*discoverd.Instance{sixth, seventh})
	assertEvent(c, events, "a", discoverd.EventKindLeader, sixth)
	c.Assert(state.GetLeader("a"), DeepEquals, sixth)

	eighth := fakeInstance()
	eighth.Index = 8
	state.AddInstance("a", eighth)

	// remove of high instance triggers no events
	state.RemoveInstance("a", eighth.ID)
	assertNoEvent(c, events)
	c.Assert(state.GetLeader("a"), DeepEquals, sixth)

	// remove of low instance triggers new leader
	state.RemoveInstance("a", sixth.ID)
	assertEvent(c, events, "a", discoverd.EventKindLeader, seventh)
	c.Assert(state.GetLeader("a"), DeepEquals, seventh)

	// remove of last instance triggers no events
	state.RemoveInstance("a", seventh.ID)
	assertNoEvent(c, events)
	c.Assert(state.GetLeader("a"), IsNil)

	// add of a new instance triggers leader
	ninth := fakeInstance()
	ninth.Index = 9
	state.AddInstance("a", ninth)
	assertEvent(c, events, "a", discoverd.EventKindLeader, ninth)
	c.Assert(state.GetLeader("a"), DeepEquals, ninth)

	// removing service triggers no events
	state.RemoveService("a")
	assertNoEvent(c, events)
	c.Assert(state.GetLeader("a"), IsNil)
}

func (StateSuite) TestGetNilService(c *C) {
	state := NewState()
	c.Assert(state.Get("a"), HasLen, 0)
}

func (StateSuite) TestSubscribeInitial(c *C) {
	for _, t := range []struct {
		name  string
		kinds discoverd.EventKind
	}{
		{
			name:  "up",
			kinds: discoverd.EventKindUp,
		},
		{
			name:  "up+update",
			kinds: discoverd.EventKindUp | discoverd.EventKindUpdate,
		},
		{
			name:  "down",
			kinds: discoverd.EventKindDown,
		},
		{
			name:  "update+down",
			kinds: discoverd.EventKindDown | discoverd.EventKindUpdate,
		},
		{
			name:  "leader",
			kinds: discoverd.EventKindLeader,
		},
		{
			name:  "leader+up",
			kinds: discoverd.EventKindLeader | discoverd.EventKindUp,
		},
		{
			name:  "leader+current",
			kinds: discoverd.EventKindLeader | discoverd.EventKindCurrent,
		},
		{
			name:  "up+leader+current",
			kinds: discoverd.EventKindUp | discoverd.EventKindLeader | discoverd.EventKindCurrent,
		},
		{
			name:  "up+current",
			kinds: discoverd.EventKindUp | discoverd.EventKindCurrent,
		},
		{
			name:  "down+current",
			kinds: discoverd.EventKindDown | discoverd.EventKindCurrent,
		},
		{
			name:  "current",
			kinds: discoverd.EventKindDown | discoverd.EventKindCurrent,
		},
	} {
		c.Log(t.name)

		// with no instances
		events := make(chan *discoverd.Event, 1)
		state := NewState()
		state.Subscribe("a", true, t.kinds, events)

		if t.kinds&discoverd.EventKindCurrent != 0 {
			assertEvent(c, events, "a", discoverd.EventKindCurrent, nil)
		}
		assertNoEvent(c, events)

		// with two instances
		one := fakeInstance()
		one.Index = 1
		two := fakeInstance()
		two.Index = 2
		state.AddInstance("a", one)
		state.AddInstance("a", two)
		events = make(chan *discoverd.Event, 4)
		state.Subscribe("a", true, t.kinds, events)
		if t.kinds&discoverd.EventKindUp != 0 {
			up := receiveSomeEvents(c, events, 2)
			assertEventEqual(c, up[one.ID][0], &discoverd.Event{
				Service:  "a",
				Kind:     discoverd.EventKindUp,
				Instance: one,
			})
			assertEventEqual(c, up[two.ID][0], &discoverd.Event{
				Service:  "a",
				Kind:     discoverd.EventKindUp,
				Instance: two,
			})
		}
		if t.kinds&discoverd.EventKindLeader != 0 {
			assertEvent(c, events, "a", discoverd.EventKindLeader, one)
		}
		if t.kinds&discoverd.EventKindCurrent != 0 {
			assertEvent(c, events, "a", discoverd.EventKindCurrent, nil)
		}
		assertNoEvent(c, events)

		// with sendCurrent false
		events = make(chan *discoverd.Event, 1)
		state.Subscribe("a", false, t.kinds, events)
		assertNoEvent(c, events)
	}
}

func (StateSuite) TestSubscribe(c *C) {
	state := NewState()

	inst1 := fakeInstance()
	inst1.Index = 1
	state.AddInstance("a", inst1)

	events := make(chan *discoverd.Event, 2)
	stream := state.Subscribe("a", true, discoverd.EventKindUp|discoverd.EventKindLeader, events)

	// initial instance
	assertEvent(c, events, "a", discoverd.EventKindUp, inst1)
	// initial leader
	assertEvent(c, events, "a", discoverd.EventKindLeader, inst1)

	inst2 := fakeInstance()
	inst2.Index = 2
	state.AddInstance("a", inst2)

	// subsequent event
	assertEvent(c, events, "a", discoverd.EventKindUp, inst2)

	stream.Close()
	_, open := <-events
	c.Assert(open, Equals, false)

	// create another update to confirm nothing is blocked
	state.AddInstance("a", fakeInstance())
}

func (StateSuite) TestBlockedSubscription(c *C) {
	state := NewState()
	events := make(chan *discoverd.Event)
	stream := state.Subscribe("a", true, discoverd.EventKindUp, events)

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

func (StateSuite) TestAddRemoveService(c *C) {
	state := NewState()

	c.Assert(state.Get("a"), IsNil)
	state.AddService("a")
	c.Assert(state.Get("a"), NotNil)
	c.Assert(state.Get("a"), HasLen, 0)

	inst := fakeInstance()
	state.AddInstance("a", inst)

	events := make(chan *discoverd.Event, 1)
	state.Subscribe("a", true, discoverd.EventKindDown, events)

	state.RemoveService("a")
	assertEvent(c, events, "a", discoverd.EventKindDown, inst)

	c.Assert(state.Get("a"), IsNil)
}

func (StateSuite) TestInstanceValid(c *C) {
	for _, t := range []struct {
		name string
		inst *discoverd.Instance
		err  string
	}{
		{
			name: "invalid proto",
			inst: &discoverd.Instance{
				ID:    md5sum("TCP-127.0.0.1:2"),
				Proto: "TCP",
				Addr:  "127.0.0.1:2",
			},
			err: discoverd.ErrInvalidProto.Error(),
		},
		{
			name: "empty proto",
			inst: &discoverd.Instance{
				ID:   md5sum("-127.0.0.1:2"),
				Addr: "127.0.0.1:2",
			},
			err: discoverd.ErrUnsetProto.Error(),
		},
		{
			name: "invalid addr",
			inst: &discoverd.Instance{
				ID:    md5sum("tcp-asdf"),
				Proto: "tcp",
				Addr:  "asdf",
			},
			err: "missing port in address asdf",
		},
		{
			name: "empty addr",
			inst: &discoverd.Instance{
				ID:    md5sum("tcp-"),
				Proto: "tcp",
				Addr:  "",
			},
			err: "missing port in address",
		},
		{
			name: "empty id",
			inst: &discoverd.Instance{
				Proto: "tcp",
				Addr:  "127.0.0.1:2",
			},
			err: "discoverd: instance id is incorrect, expected 35ee81ee2b44f7521139b75e865e3c98",
		},
		{
			name: "invalid id",
			inst: &discoverd.Instance{
				ID:    "asdf",
				Proto: "tcp",
				Addr:  "127.0.0.1:2",
			},
			err: "discoverd: instance id is incorrect, expected 35ee81ee2b44f7521139b75e865e3c98",
		},
		{
			name: "valid",
			inst: &discoverd.Instance{
				ID:    md5sum("tcp1234567890-127.0.0.1:2"),
				Proto: "tcp1234567890",
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

func (StateSuite) TestServiceNameValid(c *C) {
	for _, t := range []struct {
		name    string
		service string
		err     string
	}{
		{
			name:    "invalid service",
			service: "ASDF",
			err:     ErrInvalidService.Error(),
		},
		{
			name: "empty service",
			err:  ErrUnsetService.Error(),
		},
		{
			name:    "valid",
			service: "asdf123456-7890",
		},
	} {
		c.Log(t.name)
		err := ValidServiceName(t.service)
		if t.err == "" {
			c.Check(err, IsNil)
		} else {
			c.Assert(err, NotNil)
			c.Check(err.Error(), Equals, t.err)
		}
	}
}

func (StateSuite) TestEventKindJSON(c *C) {
	kind := struct {
		Kind discoverd.EventKind `json:"kind"`
	}{discoverd.EventKindUpdate}

	data, err := json.Marshal(kind)
	c.Assert(err, IsNil)
	c.Assert(string(data), Equals, `{"kind":"update"}`)

	err = json.Unmarshal([]byte(`{"kind":"leader"}`), &kind)
	c.Assert(err, IsNil)
	c.Assert(kind.Kind, Equals, discoverd.EventKindLeader)
}

func md5sum(data string) string {
	digest := md5.Sum([]byte(data))
	return hex.EncodeToString(digest[:])
}
