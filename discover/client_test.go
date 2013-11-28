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

	err = client.Register(serviceName, "1111", map[string]string{"foo": "bar"})
	if err != nil {
		t.Fatal("Registering service failed", err.Error())
	}
	err = client.Register(serviceName, "2222", nil)
	if err != nil {
		t.Fatal("Registering service failed", err.Error())
	}
	set, _ := client.QueryServices(serviceName)
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

	if len(set.Select(map[string]string{"id": "3"})) != 1 {
		t.Fatal("Select not returning proper services")
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

	set, _ := client.QueryServices("nonexistent")
	if len(set.Services()) != 0 {
		t.Fatal("There should be no services")
	}
}

func TestWatchesNotCalledForOfflineUpdatesToNonexistingServices(t *testing.T) {

}
