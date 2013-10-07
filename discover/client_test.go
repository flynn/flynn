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
	err = client.Register(serviceName, "1111", nil)
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
}
