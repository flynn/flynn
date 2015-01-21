package testutil

import (
	"io"
	"os"
	"os/exec"

	"github.com/flynn/flynn/discoverd/client"
	. "github.com/flynn/flynn/discoverd/testutil/etcdrunner"
)

func RunDiscoverdServer(t TestingT, port string, etcdAddr string) (string, func()) {
	killCh := make(chan struct{})
	doneCh := make(chan struct{})
	if port == "" {
		var err error
		port, err = RandomPort()
		if err != nil {
			t.Fatal("error getting random discoverd port: ", err)
		}
	}
	go func() {
		cmd := exec.Command("discoverd",
			"-http-addr", "127.0.0.1:"+port,
			"-dns-addr", "127.0.0.1:0",
			"-etcd", etcdAddr,
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

	return "127.0.0.1:" + port, func() {
		close(killCh)
		<-doneCh
	}
}

func BootDiscoverd(t TestingT, port, etcdAddr string) (*discoverd.Client, func()) {
	addr, killDiscoverd := RunDiscoverdServer(t, port, etcdAddr)

	client := discoverd.NewClientWithURL("http://" + addr)
	if err := Attempts.Run(client.Ping); err != nil {
		t.Fatal("Failed to connect to discoverd: ", err)
	}
	return client, killDiscoverd
}

func SetupDiscoverdWithEtcd(t TestingT) (*discoverd.Client, string, func()) {
	etcdAddr, killEtcd := RunEtcdServer(t)
	client, killDiscoverd := BootDiscoverd(t, "", etcdAddr)

	return client, etcdAddr, func() {
		killDiscoverd()
		killEtcd()
	}
}

func SetupDiscoverd(t TestingT) (*discoverd.Client, func()) {
	client, _, cleanup := SetupDiscoverdWithEtcd(t)
	return client, cleanup
}
