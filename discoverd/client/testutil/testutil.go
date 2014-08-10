package testutil

import (
	"bufio"
	"io"
	"io/ioutil"
	"log"
	"math/rand"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/coreos/go-etcd/etcd"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/attempt"
)

var attempts = attempt.Strategy{
	Min:   5,
	Total: 5 * time.Second,
	Delay: 200 * time.Millisecond,
}

type TestingT interface {
	Fatal(...interface{})
	Fatalf(string, ...interface{})
}

func RunEtcdServer(t TestingT) func() {
	killCh := make(chan struct{})
	doneCh := make(chan struct{})
	name := "etcd-test." + strconv.Itoa(rand.Int())
	dataDir, err := ioutil.TempDir("", "")
	if err != nil {
		t.Fatal("tempdir failed:", err)
	}
	go func() {
		cmd := exec.Command("etcd", "-name", name, "-data-dir", dataDir)
		stdout, _ := cmd.StdoutPipe()
		stderr, _ := cmd.StderrPipe()
		if err := cmd.Start(); err != nil {
			t.Fatal("etcd start failed:", err)
			return
		}
		cmdDone := make(chan error)
		go func() {
			if os.Getenv("DEBUG") != "" {
				logOutput("etcd", stdout, stderr)
			}
			cmdDone <- cmd.Wait()
		}()
		select {
		case <-killCh:
			if err := cmd.Process.Kill(); err != nil {
				t.Fatal("failed to kill etcd:", err)
				return
			}
			<-cmdDone
		case err := <-cmdDone:
			t.Fatal("etcd failed:", err)
			return
		}
		if err := os.RemoveAll(dataDir); err != nil {
			t.Fatal("etcd cleanup failed:", err)
			return
		}
		doneCh <- struct{}{}
	}()

	// wait for etcd to come up
	client := etcd.NewClient(nil)
	err = attempts.Run(func() (err error) {
		_, err = client.Get("/", false, false)
		return
	})
	if err != nil {
		t.Fatalf("Failed to connect to etcd: %q", err)
	}

	return func() {
		close(killCh)
		<-doneCh
	}
}

func logOutput(name string, rs ...io.Reader) {
	var wg sync.WaitGroup
	wg.Add(len(rs))
	for _, r := range rs {
		go func(r io.Reader) {
			scanner := bufio.NewScanner(r)
			for scanner.Scan() {
				log.Println(name+":", scanner.Text())
			}
			wg.Done()
		}(r)
	}
	wg.Wait()
}

func RunDiscoverdServer(t TestingT, addr string) func() {
	killCh := make(chan struct{})
	doneCh := make(chan struct{})
	go func() {
		cmd := exec.Command("discoverd", "-bind", addr)
		cmd.Env = append(os.Environ(), "EXTERNAL_IP=127.0.0.1")
		stderr, _ := cmd.StderrPipe()
		stdout, _ := cmd.StdoutPipe()
		if err := cmd.Start(); err != nil {
			t.Fatal("discoverd start failed:", err)
			return
		}
		cmdDone := make(chan error)
		go func() {
			if os.Getenv("DEBUG") != "" {
				logOutput("discoverd", stderr, stdout)
			}
			cmdDone <- cmd.Wait()
		}()
		select {
		case <-killCh:
			if err := cmd.Process.Kill(); err != nil {
				t.Fatal("failed to kill discoverd:", err)
				return
			}
			<-cmdDone
		case err := <-cmdDone:
			t.Fatal("discoverd failed:", err)
			return
		}
		doneCh <- struct{}{}
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
	err := attempts.Run(func() (err error) {
		client, err = discoverd.NewClientWithAddr(addr)
		return
	})
	if err != nil {
		t.Fatalf("Failed to connect to discoverd: %q", err)
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
