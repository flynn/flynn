package testutil

import (
	"io"
	"os"
	"os/exec"

	"github.com/flynn/flynn/discoverd/client"
	. "github.com/flynn/flynn/discoverd/testutil/etcdrunner"
)

func RunDiscoverdServer(t TestingT, addr string) func() {
	killCh := make(chan struct{})
	doneCh := make(chan struct{})
	go func() {
		cmd := exec.Command("discoverd", "-bind", addr)
		cmd.Env = append(os.Environ(), "EXTERNAL_IP=127.0.0.1")
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

	return func() {
		close(killCh)
		<-doneCh
	}
}

func BootDiscoverd(t TestingT, addr string) (*discoverd.Client, func()) {
	if addr == "" {
		addr = "127.0.0.1:1111"
	}
	killDiscoverd := RunDiscoverdServer(t, addr)

	var client *discoverd.Client
	err := Attempts.Run(func() (err error) {
		client, err = discoverd.NewClientWithAddr(addr)
		return
	})
	if err != nil {
		t.Fatal("Failed to connect to discoverd: ", err)
	}
	return client, killDiscoverd
}

func SetupDiscoverd(t TestingT) (*discoverd.Client, func()) {
	killEtcd := RunEtcdServer(t)
	client, killDiscoverd := BootDiscoverd(t, "")

	return client, func() {
		client.UnregisterAll()
		client.Close()
		killDiscoverd()
		killEtcd()
	}
}
