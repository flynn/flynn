package server

import (
	"net/http/httptest"
	"os"
	"time"

	. "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
	"github.com/flynn/flynn/discoverd/client"
	hh "github.com/flynn/flynn/pkg/httphelper"
)

var _ = Suite(&HTTPSuite{})

type HTTPSuite struct {
	state   *State
	backend Backend
	cleanup []func()
	server  *httptest.Server
	client  *discoverd.Client
}

func (s *HTTPSuite) SetUpTest(c *C) {
	s.cleanup = nil
	s.state = NewState()

	s.backend = newEtcdBackend(s.state)
	c.Assert(s.backend.StartSync(), IsNil)
	s.cleanup = append(s.cleanup, func() { s.backend.Close() })

	s.server = httptest.NewServer(NewHTTPHandler(NewBasicDatastore(s.state, s.backend)))
	s.cleanup = append(s.cleanup, s.server.Close)

	s.client = discoverd.NewClientWithURL(s.server.URL)

	// create and delete an instance; poor man's write barrier for service create
	events := make(chan *discoverd.Event, 1)
	stream := s.state.Subscribe("a", false, discoverd.EventKindDown, events)
	inst := fakeInstance()
	hb, err := s.client.AddServiceAndRegisterInstance("a", inst)
	c.Assert(err, IsNil)
	c.Assert(hb.Close(), IsNil)
	assertEvent(c, events, "a", discoverd.EventKindDown, inst)
	stream.Close()
}

func (s *HTTPSuite) TearDownTest(c *C) {
	for i := len(s.cleanup); i != 0; i-- {
		s.cleanup[i-1]()
	}
}

func (s *HTTPSuite) TestRegister(c *C) {
	events := make(chan *discoverd.Event, 1)
	s.state.Subscribe("a", false, discoverd.EventKindUp|discoverd.EventKindDown|discoverd.EventKindUpdate, events)

	// Ensure that register works with address expansion
	os.Setenv("EXTERNAL_IP", "127.0.0.1")
	inst := &discoverd.Instance{Addr: ":80", Proto: "tcp"}
	hb, err := s.client.Register("a", inst.Addr)
	c.Assert(err, IsNil)
	inst.Addr = "127.0.0.1:80"
	inst.ID = md5sum(inst.Proto + "-" + inst.Addr)
	assertEvent(c, events, "a", discoverd.EventKindUp, inst)
	c.Assert(hb.Addr(), Equals, inst.Addr)

	// Ensure that Close unregisters
	err = hb.Close()
	c.Assert(err, IsNil)
	assertEvent(c, events, "a", discoverd.EventKindDown, inst)

	// Ensure that RegisterInstance works
	inst.Meta = map[string]string{"foo": "bar"}
	hb, err = s.client.RegisterInstance("a", inst)
	c.Assert(err, IsNil)
	assertEvent(c, events, "a", discoverd.EventKindUp, inst)

	// Ensure that SetMeta works
	inst.Meta = map[string]string{"a": "b"}
	err = hb.SetMeta(inst.Meta)
	c.Assert(err, IsNil)
	assertEvent(c, events, "a", discoverd.EventKindUpdate, inst)

	err = hb.Close()
	c.Assert(err, IsNil)
	assertEvent(c, events, "a", discoverd.EventKindDown, inst)
}

func (s *HTTPSuite) TestWatch(c *C) {
	events := make(chan *discoverd.Event, 1)
	stream := s.state.Subscribe("a", false, discoverd.EventKindUp, events)
	inst1 := fakeInstance()
	hb1, err := s.client.RegisterInstance("a", inst1)
	c.Assert(err, IsNil)
	assertEvent(c, events, "a", discoverd.EventKindUp, inst1)
	stream.Close()

	events = make(chan *discoverd.Event)
	stream, err = s.client.Service("a").Watch(events)
	c.Assert(err, IsNil)
	defer stream.Close()

	// Ensure we get current state first
	assertEvent(c, events, "a", discoverd.EventKindUp, inst1)
	assertEvent(c, events, "a", discoverd.EventKindLeader, inst1)
	assertEvent(c, events, "a", discoverd.EventKindCurrent, nil)

	// Ensure we get updates
	inst1.Meta = map[string]string{"a": "b"}
	c.Assert(hb1.SetMeta(inst1.Meta), IsNil)
	assertEvent(c, events, "a", discoverd.EventKindUpdate, inst1)

	// Ensure we get new instances
	inst2 := fakeInstance()
	hb2, err := s.client.RegisterInstance("a", inst2)
	c.Assert(err, IsNil)
	assertEvent(c, events, "a", discoverd.EventKindUp, inst2)

	// Ensure we get down and leader events
	c.Assert(hb1.Close(), IsNil)
	assertEvent(c, events, "a", discoverd.EventKindDown, inst1)
	assertEvent(c, events, "a", discoverd.EventKindLeader, inst2)

	c.Assert(hb2.Close(), IsNil)
	assertEvent(c, events, "a", discoverd.EventKindDown, inst2)
}

func assertLeader(c *C, leaders <-chan *discoverd.Instance, expected *discoverd.Instance) {
	var actual *discoverd.Instance
	var ok bool
	select {
	case actual, ok = <-leaders:
		if !ok {
			c.Fatal("channel closed")
		}
	case <-time.After(10 * time.Second):
		c.Fatalf("timed out waiting for leader %#v", expected)
	}
	assertInstanceEqual(c, actual, expected)
}

func assertNoLeader(c *C, leaders <-chan *discoverd.Instance) {
	select {
	case inst := <-leaders:
		c.Fatalf("unexpected leader %#v", inst)
	default:
	}
}

func (s *HTTPSuite) TestLeaders(c *C) {
	// Ensure we get no leader if we register with no leader
	leaders1 := make(chan *discoverd.Instance)
	stream1, err := s.client.Service("a").Leaders(leaders1)
	c.Assert(err, IsNil)
	defer stream1.Close()
	assertNoLeader(c, leaders1)

	// Ensure we get a leader event as soon as we register
	inst1 := fakeInstance()
	hb1, err := s.client.RegisterInstance("a", inst1)
	c.Assert(err, IsNil)
	assertLeader(c, leaders1, inst1)

	// Ensure we get the current leader on a new stream
	leaders2 := make(chan *discoverd.Instance)
	stream2, err := s.client.Service("a").Leaders(leaders2)
	c.Assert(err, IsNil)
	defer stream2.Close()
	assertLeader(c, leaders2, inst1)

	inst2 := fakeInstance()
	hb2, err := s.client.RegisterInstance("a", inst2)
	c.Assert(err, IsNil)
	defer hb2.Close()
	assertNoLeader(c, leaders1)
	assertNoLeader(c, leaders2)

	// Ensure we get a new leader
	c.Assert(hb1.Close(), IsNil)
	assertLeader(c, leaders1, inst2)
	assertLeader(c, leaders2, inst2)
}

func (s *HTTPSuite) TestLeader(c *C) {
	srv := s.client.Service("a")

	// Leader with no instances 404s
	_, err := srv.Leader()
	c.Assert(discoverd.IsNotFound(err), Equals, true)

	events := make(chan *discoverd.Event, 1)
	s.state.Subscribe("a", false, discoverd.EventKindUp|discoverd.EventKindDown, events)

	// Ensure leader is returned
	inst1 := fakeInstance()
	hb1, err := s.client.RegisterInstance("a", inst1)
	c.Assert(err, IsNil)
	assertEvent(c, events, "a", discoverd.EventKindUp, inst1)
	leader, err := srv.Leader()
	c.Assert(err, IsNil)
	assertInstanceEqual(c, leader, inst1)

	// Ensure new leader is returned
	inst2 := fakeInstance()
	hb2, err := s.client.RegisterInstance("a", inst2)
	c.Assert(err, IsNil)
	defer hb2.Close()
	assertEvent(c, events, "a", discoverd.EventKindUp, inst2)
	c.Assert(hb1.Close(), IsNil)
	assertEvent(c, events, "a", discoverd.EventKindDown, inst1)
	leader, err = srv.Leader()
	c.Assert(err, IsNil)
	assertInstanceEqual(c, leader, inst2)

	// Ensure no leader is returned for service that doesn't exist
	c.Assert(s.client.RemoveService("a"), IsNil)
	assertEvent(c, events, "a", discoverd.EventKindDown, inst2)
	_, err = srv.Leader()
	c.Assert(discoverd.IsNotFound(err), Equals, true)
}

func (s *HTTPSuite) TestInstances(c *C) {
	// Instances with no service 404s
	_, err := s.client.Service("b").Instances()
	c.Assert(discoverd.IsNotFound(err), Equals, true)

	// Instances with existing service and no instances returns no results
	srv := s.client.Service("a")
	res, err := srv.Instances()
	c.Assert(err, IsNil)
	c.Assert(res, HasLen, 0)

	events := make(chan *discoverd.Event, 1)
	s.state.Subscribe("a", false, discoverd.EventKindUp|discoverd.EventKindUpdate|discoverd.EventKindDown, events)

	// Instances returns created instances
	inst1 := fakeInstance()
	hb1, err := s.client.RegisterInstance("a", inst1)
	c.Assert(err, IsNil)
	assertEvent(c, events, "a", discoverd.EventKindUp, inst1)
	inst2 := fakeInstance()
	hb2, err := s.client.RegisterInstance("a", inst2)
	c.Assert(err, IsNil)
	defer hb2.Close()
	assertEvent(c, events, "a", discoverd.EventKindUp, inst2)
	res, err = srv.Instances()
	c.Assert(err, IsNil)
	c.Assert(res, HasLen, 2)
	assertInstanceEqual(c, res[0], inst1)
	assertInstanceEqual(c, res[1], inst2)

	// Addrs returns the same result
	addrs, err := srv.Addrs()
	c.Assert(err, IsNil)
	c.Assert(addrs, DeepEquals, []string{res[0].Addr, res[1].Addr})

	// Instances reflects deleted/updated instances
	c.Assert(hb1.Close(), IsNil)
	assertEvent(c, events, "a", discoverd.EventKindDown, inst1)
	inst2.Meta = map[string]string{"a": "b"}
	c.Assert(hb2.SetMeta(inst2.Meta), IsNil)
	assertEvent(c, events, "a", discoverd.EventKindUpdate, inst2)
	res, err = srv.Instances()
	c.Assert(err, IsNil)
	c.Assert(res, HasLen, 1)
	assertInstanceEqual(c, res[0], inst2)

}

func (s *HTTPSuite) TestInstancesShortcut(c *C) {
	// Instances with no service returns after timeout
	_, err := s.client.Instances("b", time.Millisecond)
	c.Assert(err, Equals, discoverd.ErrTimedOut)

	// Instances with no current instances and timeout returns after first instance is registered
	done := make(chan struct{})
	var res []*discoverd.Instance
	go func() {
		defer close(done)
		var err error
		res, err = s.client.Instances("a", 10*time.Second)
		c.Assert(err, IsNil)
	}()
	inst1 := fakeInstance()
	hb1, err := s.client.RegisterInstance("a", inst1)
	c.Assert(err, IsNil)
	defer hb1.Close()
	<-done
	c.Assert(res, HasLen, 1)
	assertInstanceEqual(c, res[0], inst1)

	// Instances with existing instances returns them
	events := make(chan *discoverd.Event, 1)
	s.state.Subscribe("a", false, discoverd.EventKindUp, events)
	inst2 := fakeInstance()
	hb2, err := s.client.RegisterInstance("a", inst2)
	c.Assert(err, IsNil)
	defer hb2.Close()
	assertEvent(c, events, "a", discoverd.EventKindUp, inst2)
	res, err = s.client.Instances("a", 10*time.Second)
	c.Assert(res, HasLen, 2)
	assertInstanceEqual(c, res[0], inst1)
	assertInstanceEqual(c, res[1], inst2)
}

func (s *HTTPSuite) TestAddServiceAndRegister(c *C) {
	events := make(chan *discoverd.Event, 1)
	s.state.Subscribe("b", false, discoverd.EventKindUp, events)

	// Creates service
	inst := &discoverd.Instance{Addr: "127.0.0.1:1", Proto: "tcp"}
	inst.ID = md5sum(inst.Proto + "-" + inst.Addr)
	hb, err := s.client.AddServiceAndRegister("b", inst.Addr)
	c.Assert(err, IsNil)
	assertEvent(c, events, "b", discoverd.EventKindUp, inst)
	hb.Close()

	// Service already exists
	hb, err = s.client.AddServiceAndRegisterInstance("b", inst)
	c.Assert(err, IsNil)
	assertEvent(c, events, "b", discoverd.EventKindUp, inst)
	hb.Close()

	// Invalid service name
	_, err = s.client.AddServiceAndRegisterInstance("$", inst)
	c.Assert(err, NotNil)
}

func (s *HTTPSuite) TestPing(c *C) {
	c.Assert(s.client.Ping(), IsNil)
}

func (s *HTTPSuite) TestServiceMeta(c *C) {
	srv := s.client.Service("a")
	events := make(chan *discoverd.Event)
	stream, err := srv.Watch(events)
	c.Assert(err, IsNil)
	defer stream.Close()
	assertEvent(c, events, "a", discoverd.EventKindCurrent, nil)

	// Get non-existent meta
	_, err = srv.GetMeta()
	c.Assert(discoverd.IsNotFound(err), Equals, true)

	// Set new meta
	meta := &discoverd.ServiceMeta{Data: []byte("1")}
	c.Assert(srv.SetMeta(meta), IsNil)
	c.Assert(meta.Index > 0, Equals, true)
	assertMetaEvent(c, events, "a", meta)

	res, err := srv.GetMeta()
	c.Assert(err, IsNil)
	c.Assert(res, DeepEquals, meta)

	// Set meta on non-existent service
	err = s.client.Service("foo").SetMeta(meta)
	c.Assert(discoverd.IsNotFound(err), Equals, true, Commentf("err = %#v", err))

	// Update meta
	meta.Data = []byte("2")
	c.Assert(srv.SetMeta(meta), IsNil)
	c.Assert(meta.Index > res.Index, Equals, true, Commentf("old = %d, new = %d", res.Index, meta.Index))
	assertMetaEvent(c, events, "a", meta)

	// Update meta with wrong index
	meta.Index--
	err = srv.SetMeta(meta)
	c.Assert(err, NotNil)
	c.Assert(hh.IsPreconditionFailedError(err), Equals, true)
}

func (s *HTTPSuite) TestManualLeaderElection(c *C) {
	// service with auto election should not support manual leader
	err := s.client.Service("a").SetLeader("123")
	c.Assert(hh.IsValidationError(err), Equals, true)

	// create service with manual config
	err = s.client.AddService("b", &discoverd.ServiceConfig{LeaderType: discoverd.LeaderTypeManual})
	c.Assert(err, IsNil)

	// register instance
	inst1 := fakeInstance()
	hb1, err := s.client.RegisterInstance("b", inst1)
	c.Assert(err, IsNil)
	defer hb1.Close()

	// no leader event when starting watch
	events := make(chan *discoverd.Event, 2)
	s.state.Subscribe("b", true, discoverd.EventKindUp|discoverd.EventKindDown|discoverd.EventKindLeader|discoverd.EventKindCurrent, events)
	assertEvent(c, events, "b", discoverd.EventKindUp, inst1)
	assertEvent(c, events, "b", discoverd.EventKindCurrent, nil)

	// get leader should 404
	srv := s.client.Service("b")
	_, err = srv.Leader()
	c.Assert(discoverd.IsNotFound(err), Equals, true)

	// set leader
	err = srv.SetLeader(inst1.ID)
	c.Assert(err, IsNil)
	leader, err := srv.Leader()
	c.Assert(err, IsNil)
	assertInstanceEqual(c, leader, inst1)
	assertEvent(c, events, "b", discoverd.EventKindLeader, inst1)

	// add another instance and set to leader
	inst2 := fakeInstance()
	hb2, err := s.client.RegisterInstance("b", inst2)
	c.Assert(err, IsNil)
	defer hb2.Close()
	assertEvent(c, events, "b", discoverd.EventKindUp, inst2)

	err = srv.SetLeader(inst2.ID)
	c.Assert(err, IsNil)
	assertEvent(c, events, "b", discoverd.EventKindLeader, inst2)
	leader, err = srv.Leader()
	c.Assert(err, IsNil)
	assertInstanceEqual(c, leader, inst2)

	// remove leader instance
	hb2.Close()
	assertEvent(c, events, "b", discoverd.EventKindDown, inst2)
	// get leader should 404, no event
	_, err = srv.Leader()
	c.Assert(discoverd.IsNotFound(err), Equals, true)
	assertNoEvent(c, events)

	// set leader to instance 1
	err = srv.SetLeader(inst1.ID)
	c.Assert(err, IsNil)
	assertEvent(c, events, "b", discoverd.EventKindLeader, inst1)
	leader, err = srv.Leader()
	c.Assert(err, IsNil)
	assertInstanceEqual(c, leader, inst1)
}
