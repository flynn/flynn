package testutil

import (
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"time"

	"github.com/flynn/flynn/discoverd/client"
	. "github.com/flynn/flynn/discoverd/testutil/etcdrunner"
)

func RunDiscoverdServer(t TestingT, raftPort, httpPort string) (string, func()) {
	killCh := make(chan struct{})
	doneCh := make(chan struct{})

	if raftPort == "" {
		port, err := RandomPort()
		if err != nil {
			t.Fatal("error getting random discoverd raft port: ", err)
		}
		raftPort = port
	}

	if httpPort == "" {
		port, err := RandomPort()
		if err != nil {
			t.Fatal("error getting random discoverd http port: ", err)
		}
		httpPort = port
	}

	// Generate a data directory.
	dataDir, _ := ioutil.TempDir("", "testutil-")

	go func() {
		cmd := exec.Command("discoverd",
			"-host", "127.0.0.1",
			"-raft-addr", "127.0.0.1:"+raftPort,
			"-http-addr", "127.0.0.1:"+httpPort,
			"-dns-addr", "127.0.0.1:0",
			"-data-dir", dataDir,
		)

		var stderr, stdout io.Reader
		if os.Getenv("DEBUG") != "" {
			stderr, _ = cmd.StderrPipe()
			stdout, _ = cmd.StdoutPipe()
		}
		if err := cmd.Start(); err != nil {
			t.Fatal("discoverd start failed: ", err)
			return
		}
		cmdDone := make(chan error)

		go func() {
			if stdout != nil {
				LogOutput("discoverd", stderr, stdout)
			}
			cmdDone <- cmd.Wait()
		}()
		defer close(doneCh)

		select {
		case <-killCh:
			if err := cmd.Process.Kill(); err != nil {
				t.Fatal("failed to kill discoverd:", err)
				return
			}
			err := <-cmdDone
			t.Log("discoverd process exited: ", err)
		case err := <-cmdDone:
			t.Log("discoverd process exited unexpectedly: ", err)
		}
	}()

	// Ping server and wait for leadership.
	if err := waitForLeader(t, "127.0.0.1:"+httpPort, 5*time.Second); err != nil {
		t.Fatal("waiting for leader: %s", err)
	}

	return "127.0.0.1:" + httpPort, func() {
		close(killCh)
		os.RemoveAll(dataDir)
		<-doneCh
	}
}

func BootDiscoverd(t TestingT, raftPort, httpPort string) (*discoverd.Client, func()) {
	addr, killDiscoverd := RunDiscoverdServer(t, raftPort, httpPort)

	client := discoverd.NewClientWithURL("http://" + addr)
	if err := Attempts.Run(client.Ping); err != nil {
		t.Fatal("Failed to connect to discoverd: ", err)
	}
	return client, killDiscoverd
}

func SetupDiscoverd(t TestingT) (*discoverd.Client, func()) {
	client, killDiscoverd := BootDiscoverd(t, "", "")
	return client, func() {
		killDiscoverd()
	}
}

func waitForLeader(t TestingT, host string, timeout time.Duration) error {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		select {
		case <-timer.C:
			return errors.New("leadership timeout")

		case <-ticker.C:
		}

		// Ping HTTP API.
		resp, err := http.Get(fmt.Sprintf("http://%s/raft/leader", host))
		if err != nil {
			t.Log("http get error:", err)
			continue
		}
		resp.Body.Close()

		// Return successfully on 200.
		if resp.StatusCode == http.StatusOK {
			t.Log("discoverd leader established")
			return nil
		}

		// Otherwise log message that we're still waiting.
		t.Log("waiting for leader: status=", resp.StatusCode)
	}
}
