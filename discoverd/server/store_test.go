package server_test

import (
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/discoverd/server"
	"github.com/flynn/flynn/pkg/keepalive"
	"github.com/flynn/flynn/pkg/stream"
)

// Ensure the store can open and close.
func TestStore_Open(t *testing.T) {
	s := NewStore()
	if err := s.Open(); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
}

// Ensure the store returns an error when opening without a bind address.
func TestStore_Open_ErrBindAddressRequired(t *testing.T) {
	s := NewStore()
	s.Listener = nil
	if err := s.Open(); err != server.ErrListenerRequired {
		t.Fatal(err)
	}
}

// Ensure the store returns an error when opening without an advertised address.
func TestStore_Open_ErrAdvertiseRequired(t *testing.T) {
	s := NewStore()
	s.Advertise = nil
	if err := s.Open(); err != server.ErrAdvertiseRequired {
		t.Fatal(err)
	}
}

// Ensure the store can add a service.
func TestStore_AddService(t *testing.T) {
	s := MustOpenStore()
	defer s.Close()

	// Add a service.
	if err := s.AddService("service0", &discoverd.ServiceConfig{LeaderType: discoverd.LeaderTypeManual}); err != nil {
		t.Fatal(err)
	}

	// Validate that the data has been applied.
	if c := s.Config("service0"); !reflect.DeepEqual(c, &discoverd.ServiceConfig{LeaderType: discoverd.LeaderTypeManual}) {
		t.Fatalf("unexpected config: %#v", c)
	}
}

// Ensure the store uses a default config if one is not specified.
func TestStore_AddService_DefaultConfig(t *testing.T) {
	s := MustOpenStore()
	defer s.Close()

	// Add a service.
	if err := s.AddService("service0", nil); err != nil {
		t.Fatal(err)
	}

	// Validate that the data has been applied.
	if c := s.Config("service0"); !reflect.DeepEqual(c, &discoverd.ServiceConfig{LeaderType: discoverd.LeaderTypeOldest}) {
		t.Fatalf("unexpected config: %#v", c)
	}
}

// Ensure the store returns an error when creating a service that already exists.
func TestStore_AddService_ErrServiceExists(t *testing.T) {
	s := MustOpenStore()
	defer s.Close()

	// Add a service twice.
	if err := s.AddService("service0", nil); err != nil {
		t.Fatal(err)
	}
	if err := s.AddService("service0", nil); !server.IsServiceExists(err) {
		t.Fatalf("unexpected error: %v", err)
	}
}

// Ensure the store can remove an existing service.
func TestStore_RemoveService(t *testing.T) {
	s := MustOpenStore()
	defer s.Close()

	// Add services.
	if err := s.AddService("service0", nil); err != nil {
		t.Fatal(err)
	} else if err = s.AddService("service1", nil); err != nil {
		t.Fatal(err)
	} else if err = s.AddService("service2", nil); err != nil {
		t.Fatal(err)
	}

	// Remove one service.
	if err := s.RemoveService("service1"); err != nil {
		t.Fatal(err)
	}

	// Validate that only two services remain.
	if a := s.ServiceNames(); !reflect.DeepEqual(a, []string{"service0", "service2"}) {
		t.Fatalf("unexpected services: %+v", a)
	}
}

// Ensure the store sends down events when removing a service.
func TestStore_RemoveService_Events(t *testing.T) {
	s := MustOpenStore()
	defer s.Close()
	if err := s.AddService("service0", nil); err != nil {
		t.Fatal(err)
	} else if err = s.AddInstance("service0", &discoverd.Instance{ID: "inst0"}); err != nil {
		t.Fatal(err)
	} else if err = s.AddInstance("service0", &discoverd.Instance{ID: "inst1"}); err != nil {
		t.Fatal(err)
	}

	// Add subscription.
	ch := make(chan *discoverd.Event, 2)
	s.Subscribe("service0", false, discoverd.EventKindDown, ch)

	// Remove service.
	if err := s.RemoveService("service0"); err != nil {
		t.Fatal(err)
	}

	// Verify two down events were received.
	if e := <-ch; !reflect.DeepEqual(e, &discoverd.Event{Service: "service0", Kind: discoverd.EventKindDown, Instance: &discoverd.Instance{ID: "inst0", Index: 3}}) {
		t.Fatalf("unexpected event(0): %#v", e)
	}
	if e := <-ch; !reflect.DeepEqual(e, &discoverd.Event{Service: "service0", Kind: discoverd.EventKindDown, Instance: &discoverd.Instance{ID: "inst1", Index: 4}}) {
		t.Fatalf("unexpected event(1): %#v", e)
	}
}

// Ensure the store removes service meta when service is removed
func TestStore_RemoveService_RemoveMeta(t *testing.T) {
	s := MustOpenStore()
	defer s.Close()
	if err := s.AddService("service0", nil); err != nil {
		t.Fatal(err)
	}

	// Set metadata.
	if err := s.SetServiceMeta("service0", &discoverd.ServiceMeta{Data: []byte(`"foo"`), Index: 0}); err != nil {
		t.Fatal(err)
	}

	// Verify metadata was updated.
	if m := s.ServiceMeta("service0"); !reflect.DeepEqual(m, &discoverd.ServiceMeta{Data: []byte(`"foo"`), Index: 3}) {
		t.Fatalf("unexpected meta: %#v", m)
	}

	// Remove service.
	if err := s.RemoveService("service0"); err != nil {
		t.Fatal(err)
	}

	// Verify service meta is no longer set
	if m := s.ServiceMeta("service0"); m != nil {
		t.Fatalf("unexpected meta: %#v", m)
	}
}

// Ensure the store returns an error when removing non-existent services.
func TestStore_RemoveService_ErrNotFound(t *testing.T) {
	s := MustOpenStore()
	defer s.Close()
	if err := s.RemoveService("no_such_service"); !server.IsNotFound(err) {
		t.Fatalf("unexpected error: %s", err)
	}
}

// Ensure the store can add instances to a service.
func TestStore_AddInstance(t *testing.T) {
	s := MustOpenStore()
	defer s.Close()
	if err := s.AddService("service0", nil); err != nil {
		t.Fatal(err)
	}

	// Add an instances to the service.
	if err := s.AddInstance("service0", &discoverd.Instance{ID: "inst0"}); err != nil {
		t.Fatal(err)
	} else if err = s.AddInstance("service0", &discoverd.Instance{ID: "inst1"}); err != nil {
		t.Fatal(err)
	}

	// Verify that the instances exist.
	if a, err := s.Instances("service0"); err != nil {
		t.Fatal(err)
	} else if !reflect.DeepEqual(a, []*discoverd.Instance{
		{ID: "inst0", Index: 3},
		{ID: "inst1", Index: 4},
	}) {
		t.Fatalf("unexpected instances: %#v", a)
	}
}

// Ensure the store can add instances to a service.
func TestStore_AddInstance_ErrNotFound(t *testing.T) {
	s := MustOpenStore()
	defer s.Close()
	if err := s.AddInstance("no_such_service", &discoverd.Instance{ID: "inst0"}); !server.IsNotFound(err) {
		t.Fatalf("unexpected error: %s", err)
	}
}

// Ensure the store sends an "up" event when adding a new service.
func TestStore_AddInstance_UpEvent(t *testing.T) {
	s := MustOpenStore()
	defer s.Close()
	if err := s.AddService("service0", nil); err != nil {
		t.Fatal(err)
	}

	// Add subscription.
	ch := make(chan *discoverd.Event, 1)
	s.Subscribe("service0", false, discoverd.EventKindUp, ch)

	// Add instance.
	if err := s.AddInstance("service0", &discoverd.Instance{ID: "inst0"}); err != nil {
		t.Fatal(err)
	}

	// Verify "up" event was received.
	if e := <-ch; !reflect.DeepEqual(e, &discoverd.Event{
		Service:  "service0",
		Kind:     discoverd.EventKindUp,
		Instance: &discoverd.Instance{ID: "inst0", Index: 3},
	}) {
		t.Fatalf("unexpected event: %#v", e)
	}
}

// Ensure the store sends an "update" event when updating an existing service.
func TestStore_AddInstance_UpdateEvent(t *testing.T) {
	s := MustOpenStore()
	defer s.Close()
	if err := s.AddService("service0", nil); err != nil {
		t.Fatal(err)
	} else if err = s.AddInstance("service0", &discoverd.Instance{ID: "inst0", Proto: "http"}); err != nil {
		t.Fatal(err)
	}

	// Add subscription.
	ch := make(chan *discoverd.Event, 1)
	s.Subscribe("service0", false, discoverd.EventKindUpdate, ch)

	// Update instance.
	if err := s.AddInstance("service0", &discoverd.Instance{ID: "inst0", Proto: "https"}); err != nil {
		t.Fatal(err)
	}

	// Verify "update" event was received.
	if e := <-ch; !reflect.DeepEqual(e, &discoverd.Event{
		Service:  "service0",
		Kind:     discoverd.EventKindUpdate,
		Instance: &discoverd.Instance{ID: "inst0", Index: 3, Proto: "https"},
	}) {
		t.Fatalf("unexpected event: %#v", e)
	}
}

// Ensure the store sends a "leader" event when adding the first instance.
func TestStore_AddInstance_LeaderEvent(t *testing.T) {
	s := MustOpenStore()
	defer s.Close()
	if err := s.AddService("service0", nil); err != nil {
		t.Fatal(err)
	}

	// Add subscription.
	ch := make(chan *discoverd.Event, 1)
	s.Subscribe("service0", false, discoverd.EventKindLeader, ch)

	// Update instance.
	if err := s.AddInstance("service0", &discoverd.Instance{ID: "inst0"}); err != nil {
		t.Fatal(err)
	}

	// Verify "leader" event was received.
	if e := <-ch; !reflect.DeepEqual(e, &discoverd.Event{
		Service:  "service0",
		Kind:     discoverd.EventKindLeader,
		Instance: &discoverd.Instance{ID: "inst0", Index: 3},
	}) {
		t.Fatalf("unexpected event: %#v", e)
	}
}

// Ensure the store sends a "leader" event when setting the leader.
func TestStore_SetLeader_Event(t *testing.T) {
	s := MustOpenStore()
	defer s.Close()
	if err := s.AddService("service0", &discoverd.ServiceConfig{LeaderType: discoverd.LeaderTypeManual}); err != nil {
		t.Fatal(err)
	} else if err := s.AddInstance("service0", &discoverd.Instance{ID: "inst0"}); err != nil {
		t.Fatal(err)
	} else if err := s.AddInstance("service0", &discoverd.Instance{ID: "inst1"}); err != nil {
		t.Fatal(err)
	}

	// Add subscription.
	ch := make(chan *discoverd.Event, 1)
	s.Subscribe("service0", false, discoverd.EventKindLeader, ch)

	// Update instance.
	if err := s.SetServiceLeader("service0", "inst1"); err != nil {
		t.Fatal(err)
		t.Fatal(err)
	}

	// Verify "leader" event was received.
	if e := <-ch; !reflect.DeepEqual(e, &discoverd.Event{
		Service:  "service0",
		Kind:     discoverd.EventKindLeader,
		Instance: &discoverd.Instance{ID: "inst1", Index: 4},
	}) {
		t.Fatalf("unexpected event: %#v", e)
	}
}

// Ensure the store can remove an instance from a service.
func TestStore_RemoveInstance(t *testing.T) {
	s := MustOpenStore()
	defer s.Close()
	if err := s.AddService("service0", nil); err != nil {
		t.Fatal(err)
	} else if err = s.AddInstance("service0", &discoverd.Instance{ID: "inst0"}); err != nil {
		t.Fatal(err)
	} else if err = s.AddInstance("service0", &discoverd.Instance{ID: "inst1"}); err != nil {
		t.Fatal(err)
	} else if err = s.AddInstance("service0", &discoverd.Instance{ID: "inst2"}); err != nil {
		t.Fatal(err)
	}

	// Remove one instance.
	if err := s.RemoveInstance("service0", "inst1"); err != nil {
		t.Fatal(err)
	}

	// Verify the remaining instances.
	if a, err := s.Instances("service0"); err != nil {
		t.Fatal(err)
	} else if !reflect.DeepEqual(a, []*discoverd.Instance{
		{ID: "inst0", Index: 3},
		{ID: "inst2", Index: 5},
	}) {
		t.Fatalf("unexpected instances: %#v", a)
	}
}

// Ensure the store returns an error when removing an instance from a non-existent service.
func TestStore_RemoveInstance_ErrNotFound(t *testing.T) {
	s := MustOpenStore()
	defer s.Close()
	if err := s.RemoveInstance("no_such_service", "inst0"); !server.IsNotFound(err) {
		t.Fatalf("unexpected error: %s", err)
	}
}

// Ensure the store sends a "down" event when removing an existing service.
func TestStore_RemoveInstance_DownEvent(t *testing.T) {
	s := MustOpenStore()
	defer s.Close()
	if err := s.AddService("service0", nil); err != nil {
		t.Fatal(err)
	} else if err = s.AddInstance("service0", &discoverd.Instance{ID: "inst0"}); err != nil {
		t.Fatal(err)
	}

	// Add subscription.
	ch := make(chan *discoverd.Event, 1)
	s.Subscribe("service0", false, discoverd.EventKindDown, ch)

	// Remove instance.
	if err := s.RemoveInstance("service0", "inst0"); err != nil {
		t.Fatal(err)
	}

	// Verify "down" event was received.
	if e := <-ch; !reflect.DeepEqual(e, &discoverd.Event{
		Service:  "service0",
		Kind:     discoverd.EventKindDown,
		Instance: &discoverd.Instance{ID: "inst0", Index: 3},
	}) {
		t.Fatalf("unexpected event: %#v", e)
	}
}

// Ensure the store sends a "leader" event when removing an existing service.
func TestStore_RemoveInstance_LeaderEvent(t *testing.T) {
	s := MustOpenStore()
	defer s.Close()
	if err := s.AddService("service0", &discoverd.ServiceConfig{LeaderType: discoverd.LeaderTypeOldest}); err != nil {
		t.Fatal(err)
	} else if err = s.AddInstance("service0", &discoverd.Instance{ID: "inst0"}); err != nil {
		t.Fatal(err)
	} else if err = s.AddInstance("service0", &discoverd.Instance{ID: "inst1"}); err != nil {
		t.Fatal(err)
	}

	// Add subscription.
	ch := make(chan *discoverd.Event, 1)
	s.Subscribe("service0", false, discoverd.EventKindLeader, ch)

	// Remove instance, inst1 should become leader.
	if err := s.RemoveInstance("service0", "inst0"); err != nil {
		t.Fatal(err)
	}

	// Verify "leader" event was received.
	if e := <-ch; !reflect.DeepEqual(e, &discoverd.Event{
		Service:  "service0",
		Kind:     discoverd.EventKindLeader,
		Instance: &discoverd.Instance{ID: "inst1", Index: 4},
	}) {
		t.Fatalf("unexpected event: %#v", e)
	}
}

// Ensure the store can enforce expiration of instances.
func TestStore_EnforceExpiry(t *testing.T) {
	s := MustOpenStore()
	s.InstanceTTL = 100 * time.Millisecond // low TTL
	defer s.Close()

	// Add service.
	if err := s.AddService("service0", nil); err != nil {
		t.Fatal(err)
	}

	// Add subscription.
	ch := make(chan *discoverd.Event, 1)
	s.Subscribe("service0", false, discoverd.EventKindDown, ch)

	// Heartbeat instance for a little bit.
	for i := 0; i < 10; i++ {
		if err := s.AddInstance("service0", &discoverd.Instance{ID: "inst0"}); err != nil {
			t.Fatal(err)
		}
		time.Sleep(s.InstanceTTL / 2)
	}

	// Run expiry, however, instance should not be expired.
	if err := s.EnforceExpiry(); err != nil {
		t.Fatal(err)
	}

	// Verify that the instance has not been expired.
	select {
	case e := <-ch:
		t.Fatalf("unexpected event: %#v", e)
	default:
	}

	// Wait for TTL and then enforce expiry.
	time.Sleep(2 * s.InstanceTTL)
	if err := s.EnforceExpiry(); err != nil {
		t.Fatal(err)
	}

	// Verify "down" event was received.
	if e := <-ch; !reflect.DeepEqual(e, &discoverd.Event{
		Service:  "service0",
		Kind:     discoverd.EventKindDown,
		Instance: &discoverd.Instance{ID: "inst0", Index: 3},
	}) {
		t.Fatalf("unexpected event: %#v", e)
	}
}

// Ensure the store returns an error if it has not been leader for long enough.
func TestStore_EnforceExpiry_ErrLeaderWait(t *testing.T) {
	s := MustOpenStore()
	s.InstanceTTL = 10 * time.Second
	defer s.Close()

	// Attempting to expire instances before leadership has been established
	// for 2x TTL should return a "leader wait" error.
	if err := s.EnforceExpiry(); err != server.ErrLeaderWait {
		t.Fatalf("unexpected error: %s", err)
	}
}

// Ensure the store can store meta data for a service.
func TestStore_SetServiceMeta_Create(t *testing.T) {
	s := MustOpenStore()
	defer s.Close()
	if err := s.AddService("service0", nil); err != nil {
		t.Fatal(err)
	}

	// Set metadata.
	if err := s.SetServiceMeta("service0", &discoverd.ServiceMeta{Data: []byte(`"foo"`), Index: 0}); err != nil {
		t.Fatal(err)
	}

	// Verify metadata was updated.
	if m := s.ServiceMeta("service0"); !reflect.DeepEqual(m, &discoverd.ServiceMeta{Data: []byte(`"foo"`), Index: 3}) {
		t.Fatalf("unexpected meta: %#v", m)
	}
}

// Ensure the store returns an error when creating service meta that already exists.
func TestStore_SetServiceMeta_Create_ErrObjectExists(t *testing.T) {
	s := MustOpenStore()
	defer s.Close()
	if err := s.AddService("service0", nil); err != nil {
		t.Fatal(err)
	} else if err = s.SetServiceMeta("service0", &discoverd.ServiceMeta{Data: []byte(`"foo"`), Index: 0}); err != nil {
		t.Fatal(err)
	}

	// Create metadata with index=0.
	if err := s.SetServiceMeta("service0", &discoverd.ServiceMeta{Data: []byte(`"bar"`), Index: 0}); err == nil || err.Error() != `object_exists: Service metadata for "service0" already exists, use index=n to set` {
		t.Fatalf("unexpected error: %v", err)
	}
}

// Ensure the store can update existing meta data for a service.
func TestStore_SetServiceMeta_Update(t *testing.T) {
	s := MustOpenStore()
	defer s.Close()
	if err := s.AddService("service0", nil); err != nil {
		t.Fatal(err)
	} else if err = s.SetServiceMeta("service0", &discoverd.ServiceMeta{Data: []byte(`"foo"`), Index: 0}); err != nil {
		t.Fatal(err)
	}

	// Update using previous index.
	if err := s.SetServiceMeta("service0", &discoverd.ServiceMeta{Data: []byte(`"bar"`), Index: 3}); err != nil {
		t.Fatal(err)
	}

	// Verify metadata was updated.
	if m := s.ServiceMeta("service0"); !reflect.DeepEqual(m, &discoverd.ServiceMeta{Data: []byte(`"bar"`), Index: 4}) {
		t.Fatalf("unexpected meta: %#v", m)
	}
}

// Ensure the store returns an error when updating a non-existent service meta.
func TestStore_SetServiceMeta_Update_ErrPreconditionFailed_Create(t *testing.T) {
	s := MustOpenStore()
	defer s.Close()
	if err := s.AddService("service0", nil); err != nil {
		t.Fatal(err)
	}

	// Update metadata with index>0.
	if err := s.SetServiceMeta("service0", &discoverd.ServiceMeta{Data: []byte(`"foo"`), Index: 100}); err == nil || err.Error() != `precondition_failed: Service metadata for "service0" does not exist, use index=0 to set` {
		t.Fatalf("unexpected error: %v", err)
	}
}

// Ensure the store returns an error when updating service meta with the wrong CAS index.
func TestStore_SetServiceMeta_Update_ErrPreconditionFailed_WrongIndex(t *testing.T) {
	s := MustOpenStore()
	defer s.Close()
	if err := s.AddService("service0", nil); err != nil {
		t.Fatal(err)
	} else if err = s.SetServiceMeta("service0", &discoverd.ServiceMeta{Data: []byte(`"foo"`), Index: 0}); err != nil {
		t.Fatal(err)
	}

	// Update metadata with wrong previous index.
	if err := s.SetServiceMeta("service0", &discoverd.ServiceMeta{Data: []byte(`"bar"`), Index: 100}); err == nil || err.Error() != `precondition_failed: Service metadata for "service0" exists, but wrong index provided` {
		t.Fatalf("unexpected error: %v", err)
	}
}

// Ensure the store returns an error when setting metadata for a non-existent service.
func TestStore_SetServiceMeta_ErrNotFound(t *testing.T) {
	s := MustOpenStore()
	defer s.Close()
	if err := s.SetServiceMeta("service0", &discoverd.ServiceMeta{Data: []byte(`"foo"`), Index: 0}); !server.IsNotFound(err) {
		t.Fatalf("unexpected error: %s", err)
	}
}

// Ensure the store can set metadata and a leader at the same time for the service.
func TestStore_SetServiceMeta_Leader(t *testing.T) {
	s := MustOpenStore()
	defer s.Close()
	if err := s.AddService("service0", &discoverd.ServiceConfig{LeaderType: discoverd.LeaderTypeManual}); err != nil {
		t.Fatal(err)
	} else if err := s.AddInstance("service0", &discoverd.Instance{ID: "inst0"}); err != nil {
		t.Fatal(err)
	} else if err := s.AddInstance("service0", &discoverd.Instance{ID: "inst1"}); err != nil {
		t.Fatal(err)
	}

	// Add subscription.
	ch := make(chan *discoverd.Event, 2)
	s.Subscribe("service0", false, discoverd.EventKindLeader|discoverd.EventKindServiceMeta, ch)

	// Set metadata and leader.
	if err := s.SetServiceMeta("service0", &discoverd.ServiceMeta{Data: []byte(`"foo"`), LeaderID: "inst1", Index: 0}); err != nil {
		t.Fatal(err)
	}

	expected := &discoverd.ServiceMeta{Data: []byte(`"foo"`), Index: 5}
	// Verify metadata was updated.
	if m := s.ServiceMeta("service0"); !reflect.DeepEqual(m, expected) {
		t.Fatalf("unexpected meta: %#v", m)
	}

	// Verify service meta event was received.
	if e := <-ch; !reflect.DeepEqual(e, &discoverd.Event{
		Service:     "service0",
		Kind:        discoverd.EventKindServiceMeta,
		ServiceMeta: expected,
	}) {
		t.Fatalf("unexpected event: %#v", e)
	}

	// Verify leader event was received.
	if e := <-ch; !reflect.DeepEqual(e, &discoverd.Event{
		Service:  "service0",
		Kind:     discoverd.EventKindLeader,
		Instance: &discoverd.Instance{ID: "inst1", Index: 4},
	}) {
		t.Fatalf("unexpected event: %#v", e)
	}
}

// Ensure the store can manually set a leader for a manual service.
func TestStore_SetLeader(t *testing.T) {
	s := MustOpenStore()
	defer s.Close()
	if err := s.AddService("service0", &discoverd.ServiceConfig{LeaderType: discoverd.LeaderTypeManual}); err != nil {
		t.Fatal(err)
	} else if err := s.AddInstance("service0", &discoverd.Instance{ID: "inst0"}); err != nil {
		t.Fatal(err)
	} else if err := s.AddInstance("service0", &discoverd.Instance{ID: "inst1"}); err != nil {
		t.Fatal(err)
	}

	// Set the leader instance ID.
	if err := s.SetServiceLeader("service0", "inst1"); err != nil {
		t.Fatal(err)
	}

	// Verify that the leader was set.
	if inst, err := s.ServiceLeader("service0"); err != nil {
		t.Fatal(err)
	} else if !reflect.DeepEqual(inst, &discoverd.Instance{ID: "inst1", Index: 4}) {
		t.Fatalf("unexpected leader: %#v", inst)
	}
}

// Ensure the store does not error when setting a leader for a non-existent service.
func TestStore_SetLeader_NoService(t *testing.T) {
	s := MustOpenStore()
	defer s.Close()
	if err := s.SetServiceLeader("service0", "inst1"); err != nil {
		t.Fatal(err)
	} else if inst, err := s.ServiceLeader("service0"); err != nil {
		t.Fatal(err)
	} else if inst != nil {
		t.Fatalf("unexpected leader: %#v", inst)
	}
}

// Ensure the store does not error when setting a leader for a non-existent instance.
func TestStore_SetLeader_NoInstance(t *testing.T) {
	s := MustOpenStore()
	defer s.Close()
	if err := s.AddService("service0", &discoverd.ServiceConfig{LeaderType: discoverd.LeaderTypeManual}); err != nil {
		t.Fatal(err)
	}

	if err := s.SetServiceLeader("service0", "inst1"); err != nil {
		t.Fatal(err)
	} else if inst, err := s.ServiceLeader("service0"); err != nil {
		t.Fatal(err)
	} else if inst != nil {
		t.Fatalf("unexpected leader: %#v", inst)
	}
}

// Ensure the store removes blocking subscriptions.
func TestStore_Subscribe_NoBlock(t *testing.T) {
	s := MustOpenStore()
	defer s.Close()
	if err := s.AddService("service0", nil); err != nil {
		t.Fatal(err)
	}

	// Add blocking subscription.
	ch := make(chan *discoverd.Event, 0)
	s.Subscribe("service0", false, discoverd.EventKindUp, ch)

	// Add service.
	if err := s.AddInstance("service0", &discoverd.Instance{ID: "inst0"}); err != nil {
		t.Fatal(err)
	}

	// Ensure that program does not hang.
}

// Ensure the store can be restored from a snapshot
func TestStore_RestoreSnapshot(t *testing.T) {
	// open a store, add some services and trigger a snapshot
	s := MustOpenStore()
	serviceNames := []string{"service0", "service1"}
	for _, name := range serviceNames {
		if err := s.AddService(name, nil); err != nil {
			s.Close()
			t.Fatal(err)
		}
	}
	if err := s.TriggerSnapshot(); err != nil {
		s.Close()
		t.Fatal(err)
	}
	s.Store.Close()

	// open another store with same path and port which will attempt
	// to restore the snapshot
	_, port, _ := net.SplitHostPort(s.Listener.Addr().String())
	ln, err := keepalive.ReusableListen("tcp4", "127.0.0.1:"+port)
	if err != nil {
		t.Fatal(err)
	}
	s = NewStoreWithConfig(StoreConfig{Path: s.Path(), Listener: ln})
	defer s.Close()
	if err := s.Open(); err != nil {
		t.Fatal(err)
	}

	// check the data was restored
	if !reflect.DeepEqual(s.ServiceNames(), serviceNames) {
		t.Fatalf("expected service names %v, got %v", serviceNames, s.ServiceNames())
	}
}

func BenchmarkStore_AddInstance(b *testing.B) {
	s := MustOpenStore()
	defer s.Close()
	if err := s.AddService("service0", nil); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()

	// Continually heartbeat 10 instances.
	const instanceN = 10
	for i := 0; i < b.N; i++ {
		if err := s.AddInstance("service0", &discoverd.Instance{ID: fmt.Sprintf("inst%d", i%instanceN)}); err != nil {
			b.Fatal(err)
		}
	}
}

// Store represents a test wrapper for server.Store.
type Store struct {
	*server.Store
}

type StoreConfig struct {
	Path     string
	Listener net.Listener
}

// NewStore returns a new instance of Store.
func NewStore() *Store {
	return NewStoreWithConfig(StoreConfig{})
}

func NewStoreWithConfig(config StoreConfig) *Store {
	if config.Path == "" {
		// Generate a temporary path.
		f, _ := ioutil.TempFile("", "discoverd-store-")
		f.Close()
		os.Remove(f.Name())
		config.Path = f.Name()
	}

	// Initialize store.
	s := &Store{Store: server.NewStore(config.Path)}

	if config.Listener == nil {
		// Open listener on random port.
		ln, err := keepalive.ReusableListen("tcp4", "127.0.0.1:0")
		if err != nil {
			panic(err)
		}
		config.Listener = ln
	}
	_, port, _ := net.SplitHostPort(config.Listener.Addr().String())

	// Set default test settings.
	s.Listener = config.Listener
	s.Advertise, _ = net.ResolveTCPAddr("tcp", net.JoinHostPort("localhost", port))
	s.HeartbeatTimeout = 50 * time.Millisecond
	s.ElectionTimeout = 50 * time.Millisecond
	s.LeaderLeaseTimeout = 50 * time.Millisecond
	s.CommitTimeout = 5 * time.Millisecond
	s.EnableSingleNode = true

	// Turn off logs if verbose flag is not set.
	if !testing.Verbose() {
		s.LogOutput = ioutil.Discard
	}

	return s
}

// MustOpenStore returns a new, open instance of Store. Panic on error.
func MustOpenStore() *Store {
	s := NewStore()
	if err := s.Open(); err != nil {
		panic(err)
	}
	s.MustWaitForLeader()
	return s
}

// Close closes the store and removes its path.
func (s *Store) Close() error {
	defer os.RemoveAll(s.Path())
	_, err := s.Store.Close()
	return err
}

// MustWaitForLeader blocks until a leader is established. Panic on timeout.
func (s *Store) MustWaitForLeader() {
	// Wait for leadership.
	select {
	case <-time.After(30 * time.Second):
		panic("timed out waiting for leadership")
	case <-s.LeaderCh():
	}
}

// MockStore represents a mock implementation of Handler.Store.
type MockStore struct {
	LeaderFn           func() string
	IsLeaderFn         func() bool
	GetPeersFn         func() ([]string, error)
	AddPeerFn          func(peer string) error
	RemovePeerFn       func(peer string) error
	LastIndexFn        func() uint64
	AddServiceFn       func(service string, config *discoverd.ServiceConfig) error
	RemoveServiceFn    func(service string) error
	SetServiceMetaFn   func(service string, meta *discoverd.ServiceMeta) error
	ServiceMetaFn      func(service string) *discoverd.ServiceMeta
	AddInstanceFn      func(service string, inst *discoverd.Instance) error
	RemoveInstanceFn   func(service, id string) error
	InstancesFn        func(service string) ([]*discoverd.Instance, error)
	ConfigFn           func(service string) *discoverd.ServiceConfig
	SetServiceLeaderFn func(service, id string) error
	ServiceLeaderFn    func(service string) (*discoverd.Instance, error)
	SubscribeFn        func(service string, sendCurrent bool, kinds discoverd.EventKind, ch chan *discoverd.Event) stream.Stream
}

func (s *MockStore) Leader() string { return s.LeaderFn() }
func (s *MockStore) IsLeader() bool { return s.IsLeaderFn() }

func (s *MockStore) GetPeers() ([]string, error)  { return s.GetPeersFn() }
func (s *MockStore) AddPeer(peer string) error    { return s.AddPeerFn(peer) }
func (s *MockStore) RemovePeer(peer string) error { return s.RemovePeerFn(peer) }
func (s *MockStore) LastIndex() uint64            { return s.LastIndexFn() }

func (s *MockStore) AddService(service string, config *discoverd.ServiceConfig) error {
	return s.AddServiceFn(service, config)
}

func (s *MockStore) RemoveService(service string) error {
	return s.RemoveServiceFn(service)
}

func (s *MockStore) SetServiceMeta(service string, meta *discoverd.ServiceMeta) error {
	return s.SetServiceMetaFn(service, meta)
}

func (s *MockStore) ServiceMeta(service string) *discoverd.ServiceMeta {
	return s.ServiceMetaFn(service)
}

func (s *MockStore) AddInstance(service string, inst *discoverd.Instance) error {
	return s.AddInstanceFn(service, inst)
}

func (s *MockStore) RemoveInstance(service, id string) error {
	return s.RemoveInstanceFn(service, id)
}

func (s *MockStore) Instances(service string) ([]*discoverd.Instance, error) {
	return s.InstancesFn(service)
}

func (s *MockStore) Config(service string) *discoverd.ServiceConfig {
	return s.ConfigFn(service)
}

func (s *MockStore) SetServiceLeader(service, id string) error {
	return s.SetServiceLeaderFn(service, id)
}

func (s *MockStore) ServiceLeader(service string) (*discoverd.Instance, error) {
	return s.ServiceLeaderFn(service)
}

func (s *MockStore) Subscribe(service string, sendCurrent bool, kinds discoverd.EventKind, ch chan *discoverd.Event) stream.Stream {
	return s.SubscribeFn(service, sendCurrent, kinds, ch)
}
