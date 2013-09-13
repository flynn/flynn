package discover

import (
	"testing"
	"time"
)

func startServer() {
	server := NewServer()
	ListenAndServe(server)
}

func TestClient(t* testing.T) {
	t.Log("Testing Register")
	go startServer()
	time.Sleep(time.Second)
	// Giving time for the server to get online.
	client, err := NewClient()
	if err != nil {
		t.Fatal(err)
	}
	serverName := "trialServer"
	serverPort := "1234"
	// Test Registering
	err = client.Register(serverName, serverPort, make(map[string]string))
	if err != nil {
		t.Fatal("Registering client failed" + err.Error())
	}

	err = client.Register(serverName, "1222", make(map[string]string))
	serSet := client.Services(serverName)
	time.Sleep(1 * time.Second)
	online := serSet.Online()
	if(len(online) < 2) {
		t.Fatal("Registed clients not online")
	}
	//TODO  Kill The server
}



