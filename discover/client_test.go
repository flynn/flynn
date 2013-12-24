package discover

import (
	"os/exec"
	"testing"
	"time"
)

func runDiscoverdServer() func() {
	killCh := make(chan struct{})
	doneCh := make(chan struct{})
	go func() {
		cmd := exec.Command("discoverd")
		if err := cmd.Start(); err != nil {
			panic(err)
		}
		cmdDone := make(chan error)
		go func() {
			cmdDone <- cmd.Wait()
		}()
		select {
		case <-killCh:
			if err := cmd.Process.Kill(); err != nil {
				panic(err)
			}
			<-cmdDone
		case err := <-cmdDone:
			panic(err)
		}
		doneCh <- struct{}{}
	}()
	time.Sleep(200 * time.Millisecond)
	return func() {
		close(killCh)
		<-doneCh
	}
}

func TestClient(t *testing.T) {
	killEtcd := runEtcdServer()
	defer killEtcd()
	killDiscoverd := runDiscoverdServer()
	defer killDiscoverd()

	client, err := NewClient()
	if err != nil {
		t.Fatal(err)
	}
	serviceName := "testService"

	// Test Register and ServiceSet with attributes

	err = client.Register(serviceName, "1111", map[string]string{"foo": "bar"})
	if err != nil {
		t.Fatal("Registering service failed", err.Error())
	}
	err = client.Register(serviceName, "2222", nil)
	if err != nil {
		t.Fatal("Registering service failed", err.Error())
	}
	set, _ := client.ServiceSet(serviceName)
	if len(set.Services()) < 2 {
		t.Fatal("Registered services not online")
	}

	err = client.Unregister(serviceName, "2222")
	if err != nil {
		t.Fatal("Unregistering service failed", err.Error())
	}
	if len(set.Services()) != 1 {
		t.Fatal("Only 1 registered service should be left")
	}
	if set.Services()[0].Attrs["foo"] != "bar" {
		t.Fatal("Attribute not set on service as 'bar'")
	}

	// Test Re-register

	err = client.Register(serviceName, "1111", map[string]string{"foo": "baz"})
	if err != nil {
		t.Fatal("Re-registering service failed", err.Error())
	}
	if set.Services()[0].Attrs["foo"] != "baz" {
		t.Fatal("Attribute not set on re-registered service as 'baz'")
	}

	err = client.Register(serviceName, "2222", map[string]string{"foo": "qux", "id": "2"})
	if err != nil {
		t.Fatal("Registering service failed", err.Error())
	}

	// Test Filter

	set.Filter(map[string]string{"foo": "qux"})
	if len(set.Services()) > 1 {
		t.Fatal("Filter not limiting online services in set")
	}

	err = client.Register(serviceName, "3333", map[string]string{"foo": "qux", "id": "3"})
	if err != nil {
		t.Fatal("Registering service failed", err.Error())
	}
	if len(set.Services()) < 2 {
		t.Fatal("Filter not letting new matching services in set")
	}

	err = client.Register(serviceName, "4444", map[string]string{"foo": "baz"})
	if err != nil {
		t.Fatal("Registering service failed", err.Error())
	}
	if len(set.Services()) > 2 {
		t.Fatal("Filter not limiting new unmatching services from set")
	}

	// Test Select

	if len(set.Select(map[string]string{"id": "3"})) != 1 {
		t.Fatal("Select not returning proper services")
	}

	// Test Close

	err = set.Close()
	if err != nil {
		t.Fatal("Unable to close:", err)
	}

	// Test client.Services

	services, err := client.Services(serviceName)
	if err != nil {
		t.Fatal("Unable to get services:", err)
	}
	if len(services) != 4 {
		t.Fatal("Not all registered services were returned:", services)
	}

	// Test Watch with bringCurrent

	set, _ = client.ServiceSet(serviceName)
	updates := make(chan *ServiceUpdate)
	set.Watch(updates, true)
	err = client.Register(serviceName, "5555", nil)
	if err != nil {
		t.Fatal("Registering service failed", err)
	}
	for i := 0; i < 5; i++ {
		update := <-updates
		if update.Online != true {
			t.Fatal("Service update of unexected status: ", update, i)
		}
		if update.Name != serviceName {
			t.Fatal("Service update of unexected name: ", update, i)
		}
	}

}

func TestNoServices(t *testing.T) {
	killEtcd := runEtcdServer()
	defer killEtcd()
	killDiscoverd := runDiscoverdServer()
	defer killDiscoverd()

	client, err := NewClient()
	if err != nil {
		t.Fatal(err)
	}

	set, _ := client.ServiceSet("nonexistent")
	if len(set.Services()) != 0 {
		t.Fatal("There should be no services")
	}
}

func TestWatchesNotCalledForOfflineUpdatesToNonexistingServices(t *testing.T) {

}
