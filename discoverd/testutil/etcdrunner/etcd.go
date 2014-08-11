package etcdrunner

import (
	"bufio"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/coreos/go-etcd/etcd"
	"github.com/flynn/flynn/pkg/attempt"
	"github.com/flynn/flynn/pkg/random"
)

var Attempts = attempt.Strategy{
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
	name := "etcd-test." + strconv.Itoa(random.Math.Int())
	dataDir, err := ioutil.TempDir("", "")
	if err != nil {
		t.Fatal("tempdir failed:", err)
	}
	go func() {
		cmd := exec.Command("etcd", "-name", name, "-data-dir", dataDir)
		var stderr, stdout io.Reader
		if os.Getenv("DEBUG") != "" {
			stderr, _ = cmd.StderrPipe()
			stdout, _ = cmd.StdoutPipe()
		}
		if err := cmd.Start(); err != nil {
			t.Fatal("etcd start failed:", err)
			return
		}
		cmdDone := make(chan error)
		go func() {
			if stdout != nil {
				LogOutput("etcd", stdout, stderr)
			}
			cmdDone <- cmd.Wait()
		}()
		defer close(doneCh)
		select {
		case <-killCh:
			if err := cmd.Process.Kill(); err != nil {
				t.Fatal("failed to kill etcd:", err)
				return
			}
			<-cmdDone
		case <-cmdDone:
			return
		}
		if err := os.RemoveAll(dataDir); err != nil {
			return
		}
	}()

	// wait for etcd to come up
	client := etcd.NewClient(nil)
	err = Attempts.Run(func() (err error) {
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

func LogOutput(name string, rs ...io.Reader) {
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
