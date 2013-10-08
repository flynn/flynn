package discover

import (
	"runtime"
	"testing"
)

func runServer() {
	server := NewServer()
	ListenAndServe(server)
}

func TestClient(t *testing.T) {
	go runServer()
	runtime.Gosched()
	client, err := NewClient()
	if err != nil {
		t.Fatal(err)
	}
	serviceName := "testService"

	err = client.Register(serviceName, "1111", map[string]string{"foo": "bar"})
	if err != nil {
		t.Fatal("Registering service failed", err.Error())
	}
	err = client.Register(serviceName, "2222", nil)
	if err != nil {
		t.Fatal("Registering service failed", err.Error())
	}
	set := client.Services(serviceName)
	if len(set.Online()) < 2 {
		t.Fatal("Registered services not online")
	}

	err = client.Unregister(serviceName, "2222")
	if err != nil {
		t.Fatal("Unregistering service failed", err.Error())
	}
	if len(set.Online()) != 1 {
		t.Fatal("Only 1 registered service should be left")
	}
	if set.Online()[0].Attrs["foo"] != "bar" {
		t.Fatal("Attribute not set on service as 'bar'")
	}

	err = client.Register(serviceName, "1111", map[string]string{"foo": "baz"})
	if err != nil {
		t.Fatal("Re-registering service failed", err.Error())
	}
	if set.Online()[0].Attrs["foo"] != "baz" {
		t.Fatal("Attribute not set on re-registered service as 'baz'")
	}

	err = client.Register(serviceName, "2222", map[string]string{"foo": "qux", "id": "2"})
	if err != nil {
		t.Fatal("Registering service failed", err.Error())
	}

	set.Filter(map[string]string{"foo": "qux"})
	if len(set.Online()) > 1 {
		t.Fatal("Filter not limiting online services in set")
	}

	err = client.Register(serviceName, "3333", map[string]string{"foo": "qux", "id": "3"})
	if err != nil {
		t.Fatal("Registering service failed", err.Error())
	}
	if len(set.Online()) < 2 {
		t.Fatal("Filter not letting new matching services in set")
	}

	err = client.Register(serviceName, "4444", map[string]string{"foo": "baz"})
	if err != nil {
		t.Fatal("Registering service failed", err.Error())
	}
	if len(set.Online()) > 2 {
		t.Fatal("Filter not limiting new unmatching services from set")
	}

	if len(set.Select(map[string]string{"id": "3"})) != 1 {
		t.Fatal("Select not returning proper services")
	}

}
