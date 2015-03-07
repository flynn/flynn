package server

import (
	"fmt"
	"log"
	"os"
	"testing"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/coreos/go-etcd/etcd"
	. "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/discoverd/testutil/etcdrunner"
	hh "github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/random"
)

// Hook gocheck up to the "go test" runner
func Test(t *testing.T) { TestingT(t) }

var etcdAddr string

type etcdLogger struct {
	*log.Logger
}

func (l etcdLogger) Log(v ...interface{}) { l.Output(2, fmt.Sprintln(v...)) }

func TestMain(m *testing.M) {
	var cleanup func()
	etcdAddr, cleanup = etcdrunner.RunEtcdServer(etcdLogger{log.New(os.Stderr, "", log.Lmicroseconds|log.Lshortfile)})
	exitCode := m.Run()
	cleanup()
	os.Exit(exitCode)
}

type EtcdSuite struct {
	state   *State
	backend Backend
}

func newEtcdBackend(state *State) Backend {
	return NewEtcdBackend(etcd.NewClient([]string{etcdAddr}), fmt.Sprintf("/test/discoverd/%s", random.String(8)), state)
}

func (s *EtcdSuite) SetUpTest(c *C) {
	s.state = NewState()
	s.backend = newEtcdBackend(s.state)
}

func (s *EtcdSuite) TearDownTest(c *C) {
	if s.backend != nil {
		s.backend.Close()
	}
}

var _ = Suite(&EtcdSuite{})

// Sync starting with a clean slate
func (s *EtcdSuite) TestBasicSync(c *C) {
	events := make(chan *discoverd.Event, 1)
	s.state.Subscribe("a", false, discoverd.EventKindUp|discoverd.EventKindDown|discoverd.EventKindUpdate, events)

	c.Assert(s.backend.AddService("a", nil), IsNil)
	c.Assert(s.backend.StartSync(), IsNil)

	s.testBasicSync(c, events)
}

func (s *EtcdSuite) testBasicSync(c *C, events chan *discoverd.Event) {
	// Remove instance that doesn't exist
	err := s.backend.RemoveInstance("a", "b")
	c.Assert(err, DeepEquals, NotFoundError{Service: "a", Instance: "b"})

	// Create service, and use instance creation as write barrier
	err = s.backend.AddService("new-service", nil)
	c.Assert(err, IsNil)

	// Create instance
	inst := fakeInstance()
	err = s.backend.AddInstance("a", inst)
	c.Assert(err, IsNil)
	assertEvent(c, events, "a", discoverd.EventKindUp, inst)

	c.Assert(s.state.Get("new-service"), NotNil)

	// Delete service, and use instance update as write barrier
	err = s.backend.RemoveService("new-service")
	c.Assert(err, IsNil)

	// Update instance
	inst2 := *inst
	inst2.Meta = map[string]string{"a": "b"}
	err = s.backend.AddInstance("a", &inst2)
	c.Assert(err, IsNil)
	assertEvent(c, events, "a", discoverd.EventKindUpdate, &inst2)

	c.Assert(s.state.Get("new-service"), IsNil)

	// Remove instance
	err = s.backend.RemoveInstance("a", inst.ID)
	c.Assert(err, IsNil)
	assertEvent(c, events, "a", discoverd.EventKindDown, &inst2)
}

func (s *EtcdSuite) TestLeaderElection(c *C) {
	events := make(chan *discoverd.Event, 2)
	s.state.Subscribe("a", false, discoverd.EventKindLeader|discoverd.EventKindUp, events)

	c.Assert(s.backend.AddService("a", nil), IsNil)
	first := fakeInstance()
	c.Assert(s.backend.AddInstance("a", first), IsNil)

	// first instance is leader
	c.Assert(s.backend.StartSync(), IsNil)
	assertEvent(c, events, "a", discoverd.EventKindUp, first)
	assertEvent(c, events, "a", discoverd.EventKindLeader, first)

	// no event for second instance
	second := fakeInstance()
	c.Assert(s.backend.AddInstance("a", second), IsNil)
	assertEvent(c, events, "a", discoverd.EventKindUp, second)
	assertNoEvent(c, events)

	// no event for update of first instance
	update := *first
	update.Meta = map[string]string{"a": "b"}
	c.Assert(s.backend.AddInstance("a", &update), IsNil)
	assertNoEvent(c, events)

	// second instance becomes leader
	c.Assert(s.backend.RemoveInstance("a", first.ID), IsNil)
	assertEvent(c, events, "a", discoverd.EventKindLeader, second)
}

// Sync starting with empty etcd, but services in local state
func (s *EtcdSuite) TestNoServiceSync(c *C) {
	inst := fakeInstance()
	s.state.AddInstance("a", inst)

	events := make(chan *discoverd.Event, 1)
	s.state.Subscribe("a", false, discoverd.EventKindUp|discoverd.EventKindDown|discoverd.EventKindUpdate, events)

	c.Assert(s.backend.AddService("a", nil), IsNil)
	c.Assert(s.backend.StartSync(), IsNil)

	assertEvent(c, events, "a", discoverd.EventKindDown, inst)

	s.testBasicSync(c, events)
}

// Sync starting with existing, updated, deleted, added, etc services
func (s *EtcdSuite) TestLocalDiffSync(c *C) {
	existing := fakeInstance()
	updated := fakeInstance()
	deleted := fakeInstance()
	added := fakeInstance()
	missingService := fakeInstance()

	s.state.AddService("existing", DefaultServiceConfig)
	s.state.AddService("deleted", DefaultServiceConfig)

	s.state.AddInstance("a", existing)
	s.state.AddInstance("a", updated)
	s.state.AddInstance("a", deleted)
	s.state.AddInstance("b", missingService)

	s.state.SetServiceMeta("a", []byte("existing"), 1)

	updated2 := *updated
	updated2.Meta = map[string]string{"a": "b"}
	c.Assert(s.backend.AddService("a", nil), IsNil)
	c.Assert(s.backend.AddService("existing", nil), IsNil)
	c.Assert(s.backend.AddService("new", nil), IsNil)
	c.Assert(s.backend.AddInstance("a", existing), IsNil)
	c.Assert(s.backend.AddInstance("a", &updated2), IsNil)
	c.Assert(s.backend.AddInstance("a", added), IsNil)

	c.Assert(s.backend.SetServiceMeta("a", &discoverd.ServiceMeta{Data: []byte("new")}), IsNil)

	aEvents := make(chan *discoverd.Event, 4)
	bEvents := make(chan *discoverd.Event, 2)
	s.state.Subscribe("a", false, discoverd.EventKindUp|discoverd.EventKindDown|discoverd.EventKindUpdate|discoverd.EventKindServiceMeta, aEvents)
	s.state.Subscribe("b", false, discoverd.EventKindDown, bEvents)

	c.Assert(s.backend.StartSync(), IsNil)

	assertMetaEvent(c, aEvents, "a", &discoverd.ServiceMeta{Data: []byte("new")})

	// Ensure that a service that is not in etcd is removed
	assertEvent(c, bEvents, "b", discoverd.EventKindDown, missingService)

	// Ensure that service sync works
	c.Assert(s.state.Get("existing"), NotNil)
	c.Assert(s.state.Get("new"), NotNil)
	c.Assert(s.state.Get("deleted"), IsNil)

	res := receiveEvents(c, aEvents, 3)
	assertEventEqual(c, res[updated.ID][0], &discoverd.Event{
		Service:  "a",
		Kind:     discoverd.EventKindUpdate,
		Instance: &updated2,
	})
	assertEventEqual(c, res[deleted.ID][0], &discoverd.Event{
		Service:  "a",
		Kind:     discoverd.EventKindDown,
		Instance: deleted,
	})
	assertEventEqual(c, res[added.ID][0], &discoverd.Event{
		Service:  "a",
		Kind:     discoverd.EventKindUp,
		Instance: added,
	})

	s.testBasicSync(c, aEvents)
}

func (s *EtcdSuite) TestServiceAddRemove(c *C) {
	err := s.backend.RemoveService("a")
	c.Assert(err, DeepEquals, NotFoundError{Service: "a"})

	err = s.backend.AddService("a", nil)
	c.Assert(err, IsNil)

	err = s.backend.AddService("a", nil)
	c.Assert(err, DeepEquals, ServiceExistsError("a"))

	err = s.backend.RemoveService("a")
	c.Assert(err, IsNil)

	err = s.backend.RemoveService("a")
	c.Assert(err, DeepEquals, NotFoundError{Service: "a"})
}

func (s *EtcdSuite) TestLeaderElectionCreatedIndex(c *C) {
	c.Assert(s.backend.AddService("a", nil), IsNil)

	inst1, inst2 := fakeInstance(), fakeInstance()
	c.Assert(s.backend.AddInstance("a", inst1), IsNil)
	c.Assert(s.backend.AddInstance("a", inst2), IsNil)
	inst1.Meta = map[string]string{"a": "b"}
	c.Assert(s.backend.AddInstance("a", inst1), IsNil)

	events := make(chan *discoverd.Event, 1)
	s.state.Subscribe("a", false, discoverd.EventKindLeader, events)
	c.Assert(s.backend.StartSync(), IsNil)
	assertEvent(c, events, "a", discoverd.EventKindLeader, inst1)
}

func (s *EtcdSuite) TestSetMeta(c *C) {
	events := make(chan *discoverd.Event, 1)
	s.state.Subscribe("a", false, discoverd.EventKindServiceMeta, events)

	c.Assert(s.backend.AddService("a", nil), IsNil)
	c.Assert(s.backend.StartSync(), IsNil)

	// with service that doesn't exist
	err := s.backend.SetServiceMeta("b", &discoverd.ServiceMeta{Data: []byte("foo")})
	c.Assert(err, FitsTypeOf, NotFoundError{})

	// new with wrong index
	err = s.backend.SetServiceMeta("a", &discoverd.ServiceMeta{Data: []byte("foo"), Index: 1})
	c.Assert(err, FitsTypeOf, hh.JSONError{})
	c.Assert(err.(hh.JSONError).Code, Equals, hh.PreconditionFailedError)

	// new
	meta := &discoverd.ServiceMeta{Data: []byte("foo"), Index: 0}
	c.Assert(s.backend.SetServiceMeta("a", meta), IsNil)
	assertMetaEvent(c, events, "a", meta)

	// index=0 set with existing
	err = s.backend.SetServiceMeta("a", &discoverd.ServiceMeta{Data: []byte("foo"), Index: 0})
	c.Assert(err, FitsTypeOf, hh.JSONError{})
	c.Assert(err.(hh.JSONError).Code, Equals, hh.ObjectExistsError)

	// set with existing, valid index
	meta.Data = []byte("bar")
	c.Assert(s.backend.SetServiceMeta("a", meta), IsNil)
	assertMetaEvent(c, events, "a", meta)

	// set with existing, low index
	meta.Index--
	meta.Data = []byte("baz")
	err = s.backend.SetServiceMeta("a", meta)
	c.Assert(err, FitsTypeOf, hh.JSONError{})
	c.Assert(err.(hh.JSONError).Code, Equals, hh.PreconditionFailedError)
}

func (s *EtcdSuite) TestManualLeaderInitialSync(c *C) {
	events := make(chan *discoverd.Event, 1)
	s.state.Subscribe("a", false, discoverd.EventKindLeader, events)

	c.Assert(s.backend.AddService("a", &discoverd.ServiceConfig{LeaderType: discoverd.LeaderTypeManual}), IsNil)
	inst1, inst2 := fakeInstance(), fakeInstance()
	c.Assert(s.backend.AddInstance("a", inst1), IsNil)
	c.Assert(s.backend.AddInstance("a", inst2), IsNil)
	c.Assert(s.backend.SetLeader("a", inst2.ID), IsNil)

	c.Assert(s.backend.StartSync(), IsNil)
	assertEvent(c, events, "a", discoverd.EventKindLeader, inst2)
}

func (s *EtcdSuite) TestManualLeaderInitialSyncDelayedRegister(c *C) {
	events := make(chan *discoverd.Event, 1)
	s.state.Subscribe("a", false, discoverd.EventKindLeader, events)

	c.Assert(s.backend.AddService("a", &discoverd.ServiceConfig{LeaderType: discoverd.LeaderTypeManual}), IsNil)
	inst1 := fakeInstance()
	c.Assert(s.backend.SetLeader("a", inst1.ID), IsNil)

	c.Assert(s.backend.StartSync(), IsNil)
	c.Assert(s.backend.AddInstance("a", inst1), IsNil)
	assertEvent(c, events, "a", discoverd.EventKindLeader, inst1)
	assertInstanceEqual(c, s.state.GetLeader("a"), inst1)
}
