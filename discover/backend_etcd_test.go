package discover

import (
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/coreos/go-etcd/etcd"
)

func touchService(client *etcd.Client, service string, addr string) {
	client.Set(fmt.Sprintf("/services/%s/%s", service, addr), addr, 0)
}

func deleteService(client *etcd.Client, service string, addr string) {
	client.Delete(fmt.Sprintf("/services/%s/%s", service, addr))
}

func TestEtcdBackend_RegisterAndUnregister(t *testing.T) {

	// TODO Create server here itself and connect to it.
	client := etcd.NewClient()
	backend := EtcdBackend{Client: client}
	serviceName := "test_register"
	serviceAddr := "127.0.0.1"

	deleteService(client, serviceName, serviceAddr)
	t.Log("Testing Register")
	backend.Register(serviceName, serviceAddr, make(map[string]string))

	getUrl := "/services/" + serviceName + "/" + serviceAddr
	results, err := client.Get(getUrl)
	if err != nil {
		t.Fatal(err)
	}

	// Adding the case where the result is checked.
	if len(results) < 1 {
		t.Fatal("Flynn Error: No Response From Server")
	} else {
		// Check if the files the returned values are the same.
		if(results[0].Key != getUrl) || (results[0].Value != serviceAddr) {
			t.Fatal("Returned value not equal to sent one")
		}
	}

	t.Log("Testing Unregister of etcd backend")
	backend.Unregister("test_register", "127.0.0.1")
	_, err = client.Get("/services/test_register/127.0.0.1")
	if err == nil {
		t.Fatal("Value not deleted after unregister")
	}
}

func TestEtcdBackend_Subscribe(t *testing.T) {
	client := etcd.NewClient()
	backend := EtcdBackend{Client: client}

	backend.Register("test_subscribe", "10.0.0.1", map[string]string{})
	defer backend.Unregister("test_subscribe", "10.0.0.1")
	backend.Register("test_subscribe", "10.0.0.2", map[string]string{})
	defer backend.Unregister("test_subscribe", "10.0.0.2")

	updates, _ := backend.Subscribe("test_subscribe")
	runtime.Gosched()

	backend.Register("test_subscribe", "10.0.0.3", map[string]string{})
	defer backend.Unregister("test_subscribe", "10.0.0.3")
	backend.Register("test_subscribe", "10.0.0.4", map[string]string{})
	defer backend.Unregister("test_subscribe", "10.0.0.4")

	for i := 0; i < 4; i++ {
		addr := (<-updates).Addr
		if !strings.Contains("10.0.0.1 10.0.0.2 10.0.0.3 10.0.0.4", addr) {
			t.Fatal("Service update of unexected addr: ", addr)
		}
	}

	backend.Register("test_subscribe", "10.0.0.5", map[string]string{})
	backend.Unregister("test_subscribe", "10.0.0.5")

	<-updates           // .5 comes online
	update := <-updates // .5 goes offline
	if update.Addr != "10.0.0.5" {
		t.Fatal("Unexpected addr: ", update)
	}
	if update.Online != false {
		t.Fatal("Expected service to be offline:", update)
	}
}
