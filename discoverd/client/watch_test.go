package discoverd_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/discoverd/testutil"
	. "github.com/flynn/go-check"
)

// Hook gocheck up to the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type ClientSuite struct{}

var _ = Suite(&ClientSuite{})

func (s *ClientSuite) TestWatchReconnect(c *C) {
	c.Skip("fix discoverd watch reconnect") // FIXME(benbjohnson)

	port, err := testutil.RandomPort()
	c.Assert(err, IsNil)

	// clientA is used to register services and instances, and remains connected
	clientA, cleanup := testutil.SetupDiscoverd(c)
	defer cleanup()

	// clientB is connected to the server which will be restarted, and is used to
	// test that the watch generates the correct events after reconnecting
	clientB, killDiscoverd := testutil.BootDiscoverd(c, port)
	defer func() { killDiscoverd() }()

	// create a service with manual leader and some metadata
	service := "foo"
	config := &discoverd.ServiceConfig{LeaderType: discoverd.LeaderTypeManual}
	c.Assert(clientA.AddService(service, config), IsNil)
	serviceMeta := &discoverd.ServiceMeta{Data: []byte(`{"foo": "bar"}`)}
	c.Assert(clientA.Service(service).SetMeta(serviceMeta), IsNil)

	register := func(client *discoverd.Client, addr string, meta map[string]string) (discoverd.Heartbeater, *discoverd.Instance) {
		inst := &discoverd.Instance{Addr: addr, Proto: "tcp", Meta: meta}
		hb, err := client.RegisterInstance(service, inst)
		c.Assert(err, IsNil)
		return hb, inst
	}
	waitForEvent := func(events chan *discoverd.Event, addr string, kind discoverd.EventKind) {
		for {
			select {
			case e := <-events:
				if e.Kind == kind && (addr == "" || addr == e.Instance.Addr) {
					return
				}
			case <-time.After(10 * time.Second):
				c.Fatalf("timed out wating for %s event", kind)
			}
		}
	}
	waitForWatchState := func(ch chan discoverd.WatchState, state discoverd.WatchState) {
		for {
			select {
			case s := <-ch:
				if s == state {
					return
				}
			case <-time.After(10 * time.Second):
				c.Fatalf("timed out waiting for watch %s state", state)
			}
		}
	}

	// register three services
	register(clientA, ":1111", nil)
	hb2, _ := register(clientA, ":2222", map[string]string{"foo": "bar"})
	hb3, _ := register(clientA, ":3333", nil)

	// create watches using both clients so we can synchronize assertions
	eventsA := make(chan *discoverd.Event)
	watchA, err := clientA.Service(service).Watch(eventsA)
	c.Assert(err, IsNil)
	defer watchA.Close()
	waitForEvent(eventsA, "", discoverd.EventKindCurrent)

	eventsB := make(chan *discoverd.Event)
	watchB, err := clientB.Service(service).Watch(eventsB)
	c.Assert(err, IsNil)
	defer watchB.Close()
	waitForEvent(eventsB, "", discoverd.EventKindCurrent)

	// kill clientB's server and wait for the watch to disconnect
	stateCh := make(chan discoverd.WatchState)
	watchB.(*discoverd.Watch).SetStateChannel(stateCh)
	killDiscoverd()
	waitForWatchState(stateCh, discoverd.WatchStateDisconnected)

	// make some changes using clientA

	// change some metadata
	c.Assert(hb2.SetMeta(map[string]string{"foo": "baz"}), IsNil)
	waitForEvent(eventsA, ":2222", discoverd.EventKindUpdate)

	// register a new instance
	_, inst := register(clientA, ":4444", nil)
	waitForEvent(eventsA, ":4444", discoverd.EventKindUp)

	// set a new leader
	clientA.Service(service).SetLeader(inst.ID)
	waitForEvent(eventsA, ":4444", discoverd.EventKindLeader)

	// unregister an instance
	hb3.Close()
	waitForEvent(eventsA, ":3333", discoverd.EventKindDown)

	// update the service metadata
	serviceMeta.Data = []byte(`{"foo": "baz"}`)
	c.Assert(clientA.Service(service).SetMeta(serviceMeta), IsNil)
	waitForEvent(eventsA, "", discoverd.EventKindServiceMeta)

	// restart clientB's server and wait for the watch to reconnect
	_, killDiscoverd = testutil.RunDiscoverdServer(c, port)
	waitForWatchState(stateCh, discoverd.WatchStateConnected)

	type expectedEvent struct {
		Addr        string
		Kind        discoverd.EventKind
		ServiceMeta *discoverd.ServiceMeta
	}

	assertCurrent := func(events chan *discoverd.Event, expected []*expectedEvent) {
		count := 0
		isExpected := func(event *discoverd.Event) bool {
			for _, e := range expected {
				if e.Kind != event.Kind {
					continue
				}
				switch event.Kind {
				case discoverd.EventKindServiceMeta:
					if reflect.DeepEqual(event.ServiceMeta, e.ServiceMeta) {
						return true
					}
				default:
					if event.Instance != nil && event.Instance.Addr == e.Addr {
						return true
					}
				}
			}
			return false
		}
		for {
			select {
			case event := <-events:
				if event.Kind == discoverd.EventKindCurrent {
					if count != len(expected) {
						c.Fatalf("expected %d events, got %d", len(expected), count)
					}
					return
				}
				if !isExpected(event) {
					c.Fatalf("unexpected event: %+v", event)
				}
				count++
			case <-time.After(10 * time.Second):
				c.Fatal("timed out waiting for events")
			}
		}
	}

	// check watchB emits missed events
	assertCurrent(eventsB, []*expectedEvent{
		{Addr: ":2222", Kind: discoverd.EventKindUpdate},
		{Addr: ":4444", Kind: discoverd.EventKindUp},
		{Addr: ":4444", Kind: discoverd.EventKindLeader},
		{Kind: discoverd.EventKindServiceMeta, ServiceMeta: serviceMeta},
		{Addr: ":3333", Kind: discoverd.EventKindDown},
	})
}
