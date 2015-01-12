package server

import (
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/coreos/go-etcd/etcd"
	. "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
	"github.com/flynn/flynn/discoverd/testutil/etcdrunner"
)

type EtcdSuite struct {
	state   *State
	backend Backend
	cleanup func()
}

func (s *EtcdSuite) SetUpTest(c *C) {
	var addr string
	addr, s.cleanup = etcdrunner.RunEtcdServer(c)
	s.state = NewState()
	s.backend = NewEtcdBackend(etcd.NewClient([]string{addr}), "/test/discoverd", s.state)
}

func (s *EtcdSuite) TearDownTest(c *C) {
	if s.backend != nil {
		s.backend.Close()
	}
	if s.cleanup != nil {
		s.cleanup()
	}
}

var _ = Suite(&EtcdSuite{})

// Sync starting with a clean slate
func (s *EtcdSuite) TestBasicSync(c *C) {
	events := make(chan *Event, 1)
	s.state.Subscribe("a", false, EventKindUp|EventKindDown|EventKindUpdate, events)

	c.Assert(s.backend.AddService("a"), IsNil)
	c.Assert(s.backend.StartSync(), IsNil)

	s.testBasicSync(c, events)
}

func (s *EtcdSuite) testBasicSync(c *C, events chan *Event) {
	// Remove instance that doesn't exist
	err := s.backend.RemoveInstance("a", "b")
	c.Assert(err, DeepEquals, NotFoundError{Service: "a", Instance: "b"})

	// Create service, and use instance creation as write barrier
	err = s.backend.AddService("new-service")
	c.Assert(err, IsNil)

	// Create instance
	inst := fakeInstance()
	err = s.backend.AddInstance("a", inst)
	c.Assert(err, IsNil)
	assertEvent(c, events, "a", EventKindUp, inst)

	c.Assert(s.state.Get("new-service"), NotNil)

	// Delete service, and use instance update as write barrier
	err = s.backend.RemoveService("new-service")
	c.Assert(err, IsNil)

	// Update instance
	inst2 := *inst
	inst2.Meta = map[string]string{"a": "b"}
	err = s.backend.AddInstance("a", &inst2)
	c.Assert(err, IsNil)
	assertEvent(c, events, "a", EventKindUpdate, &inst2)

	c.Assert(s.state.Get("new-service"), IsNil)

	// Remove instance
	err = s.backend.RemoveInstance("a", inst.ID)
	c.Assert(err, IsNil)
	assertEvent(c, events, "a", EventKindDown, &inst2)
}

func (s *EtcdSuite) TestLeaderElection(c *C) {
	events := make(chan *Event, 2)
	s.state.Subscribe("a", false, EventKindLeader|EventKindUp, events)

	c.Assert(s.backend.AddService("a"), IsNil)
	first := fakeInstance()
	c.Assert(s.backend.AddInstance("a", first), IsNil)

	// first instance is leader
	c.Assert(s.backend.StartSync(), IsNil)
	assertEvent(c, events, "a", EventKindUp, first)
	assertEvent(c, events, "a", EventKindLeader, first)

	// no event for second instance
	second := fakeInstance()
	c.Assert(s.backend.AddInstance("a", second), IsNil)
	assertEvent(c, events, "a", EventKindUp, second)
	assertNoEvent(c, events)

	// no event for update of first instance
	update := *first
	update.Meta = map[string]string{"a": "b"}
	c.Assert(s.backend.AddInstance("a", &update), IsNil)
	assertNoEvent(c, events)

	// second instance becomes leader
	c.Assert(s.backend.RemoveInstance("a", first.ID), IsNil)
	assertEvent(c, events, "a", EventKindLeader, second)
}

// Sync starting with empty etcd, but services in local state
func (s *EtcdSuite) TestNoServiceSync(c *C) {
	inst := fakeInstance()
	s.state.AddInstance("a", inst)

	events := make(chan *Event, 1)
	s.state.Subscribe("a", false, EventKindUp|EventKindDown|EventKindUpdate, events)

	c.Assert(s.backend.AddService("a"), IsNil)
	c.Assert(s.backend.StartSync(), IsNil)

	assertEvent(c, events, "a", EventKindDown, inst)

	s.testBasicSync(c, events)
}

// Sync starting with existing, updated, deleted, added, etc services
func (s *EtcdSuite) TestLocalDiffSync(c *C) {
	existing := fakeInstance()
	updated := fakeInstance()
	deleted := fakeInstance()
	added := fakeInstance()
	missingService := fakeInstance()

	s.state.AddService("existing")
	s.state.AddService("deleted")

	s.state.AddInstance("a", existing)
	s.state.AddInstance("a", updated)
	s.state.AddInstance("a", deleted)
	s.state.AddInstance("b", missingService)

	updated2 := *updated
	updated2.Meta = map[string]string{"a": "b"}
	c.Assert(s.backend.AddService("a"), IsNil)
	c.Assert(s.backend.AddService("existing"), IsNil)
	c.Assert(s.backend.AddService("new"), IsNil)
	c.Assert(s.backend.AddInstance("a", existing), IsNil)
	c.Assert(s.backend.AddInstance("a", &updated2), IsNil)
	c.Assert(s.backend.AddInstance("a", added), IsNil)

	aEvents := make(chan *Event, 3)
	bEvents := make(chan *Event, 1)
	s.state.Subscribe("a", false, EventKindUp|EventKindDown|EventKindUpdate, aEvents)
	s.state.Subscribe("b", false, EventKindDown, bEvents)

	c.Assert(s.backend.StartSync(), IsNil)

	// Ensure that a service that is not in etcd is removed
	assertEvent(c, bEvents, "b", EventKindDown, missingService)

	// Ensure that service sync works
	c.Assert(s.state.Get("existing"), NotNil)
	c.Assert(s.state.Get("new"), NotNil)
	c.Assert(s.state.Get("deleted"), IsNil)

	res := receiveEvents(c, aEvents, 3)
	assertEventEqual(c, res[updated.ID][0], &Event{
		Service:  "a",
		Kind:     EventKindUpdate,
		Instance: &updated2,
	})
	assertEventEqual(c, res[deleted.ID][0], &Event{
		Service:  "a",
		Kind:     EventKindDown,
		Instance: deleted,
	})
	assertEventEqual(c, res[added.ID][0], &Event{
		Service:  "a",
		Kind:     EventKindUp,
		Instance: added,
	})

	s.testBasicSync(c, aEvents)
}

func (s *EtcdSuite) TestServiceAddRemove(c *C) {
	err := s.backend.RemoveService("a")
	c.Assert(err, DeepEquals, NotFoundError{Service: "a"})

	err = s.backend.AddService("a")
	c.Assert(err, IsNil)

	err = s.backend.AddService("a")
	c.Assert(err, DeepEquals, ServiceExistsError("a"))

	err = s.backend.RemoveService("a")
	c.Assert(err, IsNil)

	err = s.backend.RemoveService("a")
	c.Assert(err, DeepEquals, NotFoundError{Service: "a"})
}
