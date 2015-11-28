package discoverd

import (
	"reflect"
	"testing"
	"time"

	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/discoverd/testutil"
	"github.com/flynn/flynn/flannel/subnet"
)

type test struct {
	*testing.T

	client   *discoverd.Client
	service  discoverd.Service
	events   chan *discoverd.Event
	registry subnet.Registry

	cleanup func()
}

func newTest(t *testing.T) (s *test) {
	defer func() {
		if t.Failed() {
			s.cleanup()
			t.FailNow()
		}
	}()

	client, killDiscoverd := testutil.BootDiscoverd(t, "")

	serviceName := "flannel-test"
	if err := client.AddService(serviceName, nil); err != nil {
		t.Errorf("error adding service: %s", err)
	}

	service := client.Service(serviceName)
	events := make(chan *discoverd.Event)
	stream, err := service.Watch(events)
	if err != nil {
		t.Errorf("error creating watch: %s", err)
	}

	data := []byte(`{"config":{"network": "10.3.0.0/16"}}`)
	if err := service.SetMeta(&discoverd.ServiceMeta{Data: data}); err != nil {
		t.Errorf("error setting meta: %s", err)
	}

	registry, err := NewRegistry(client, serviceName)
	if err != nil {
		t.Errorf("error creating registry: %s", err)
	}

	return &test{
		T:        t,
		client:   client,
		service:  service,
		events:   events,
		registry: registry,
		cleanup: func() {
			stream.Close()
			killDiscoverd()
		},
	}
}

func (t *test) assertSubnets(expected map[string][]byte) {
loop:
	for {
		select {
		case event := <-t.events:
			if event.Kind == discoverd.EventKindServiceMeta {
				break loop
			}
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for discoverd event")
		}
	}
	res, err := t.registry.GetSubnets()
	if err != nil {
		t.Fatalf("error getting subnets: %s", err)
	}
	if !reflect.DeepEqual(res.Subnets, expected) {
		t.Fatalf("unexpected subnets, expected %s, got %s", expected, res.Subnets)
	}
}

func TestGetConfig(t *testing.T) {
	s := newTest(t)
	defer s.cleanup()

	actual, err := s.registry.GetConfig()
	if err != nil {
		t.Fatalf("error getting config: %s", err)
	}

	expected := `{"network":"10.3.0.0/16"}`
	if !reflect.DeepEqual(actual, []byte(expected)) {
		t.Fatalf("unexpected config, expected %s, got %s", expected, string(actual))
	}
}

func TestCreateSubnet(t *testing.T) {
	s := newTest(t)
	defer s.cleanup()

	_, err := s.registry.CreateSubnet("10.3.1.0-24", `{"PublicIP":"1.2.3.4"}`, 0)
	if err != nil {
		t.Fatalf("error creating subnet: %s", err)
	}
	s.assertSubnets(map[string][]byte{"10.3.1.0-24": []byte(`{"PublicIP":"1.2.3.4"}`)})

	_, err = s.registry.CreateSubnet("10.3.1.0-24", `{"PublicIP":"1.2.3.4"}`, 0)
	if err != subnet.ErrSubnetExists {
		t.Fatalf("expected ErrSubnetExists, got: %s", err)
	}
}

func TestUpdateSubnet(t *testing.T) {
	s := newTest(t)
	defer s.cleanup()

	_, err := s.registry.CreateSubnet("10.3.1.0-24", `{"PublicIP":"1.2.3.4"}`, 0)
	if err != nil {
		t.Fatalf("error creating subnet: %s", err)
	}
	s.assertSubnets(map[string][]byte{"10.3.1.0-24": []byte(`{"PublicIP":"1.2.3.4"}`)})

	_, err = s.registry.UpdateSubnet("10.3.1.0-24", `{"PublicIP":"4.5.6.7"}`, 0)
	if err != nil {
		t.Fatalf("error updating subnet: %s", err)
	}
	s.assertSubnets(map[string][]byte{"10.3.1.0-24": []byte(`{"PublicIP":"4.5.6.7"}`)})
}

func TestWatchSubnets(t *testing.T) {
	s := newTest(t)
	defer s.cleanup()

	createRes, err := s.registry.CreateSubnet("10.3.1.0-24", `{"PublicIP":"1.2.3.4"}`, 0)
	if err != nil {
		t.Fatalf("error creating subnet: %s", err)
	}
	s.assertSubnets(map[string][]byte{"10.3.1.0-24": []byte(`{"PublicIP":"1.2.3.4"}`)})

	_, err = s.registry.CreateSubnet("10.3.2.0-24", `{"PublicIP":"4.5.6.7"}`, 0)
	if err != nil {
		t.Fatalf("error creating subnet: %s", err)
	}

	res, err := s.registry.WatchSubnets(createRes.Index+1, make(chan bool))
	if err != nil {
		t.Fatalf("error watching subnets: %s", err)
	}
	expected := map[string][]byte{"10.3.2.0-24": []byte(`{"PublicIP":"4.5.6.7"}`)}
	if !reflect.DeepEqual(res.Subnets, expected) {
		t.Fatalf("unexpected subnets, expected %s, got %s", expected, res.Subnets)
	}
}
