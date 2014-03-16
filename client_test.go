package discoverd

import (
	"bufio"
	"io"
	"log"
	"math/rand"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/flynn/discoverd/agent"
)

func ExampleRegisterAndStandby_standby() {
	standbyCh, err := RegisterAndStandby("sampi", ":9099", nil)
	if err != nil {
		panic(err)
	}
	<-standbyCh
	// run server
}

func ExampleRegisterAndStandby_upgrade() {
	standbyCh, err := RegisterAndStandby("sampi", ":9099", nil)
	if err != nil {
		panic(err)
	}
	go func() {
		<-standbyCh
		// upgrade to leader
	}()
	// run server
}

func ExampleServiceSet_Leaders_client() {
	set, err := NewServiceSet("sampi")
	if err != nil {
		panic(err)
	}
	leaders := set.Leaders()
	go func() {
		for newLeader := range leaders {
			println(newLeader)
			// update connection to connect to newLeader.Addr
		}
	}()
}

func ExampleServiceSet_Watch_updatePool() {
	set, err := NewServiceSet("app")
	if err != nil {
		panic(err)
	}
	go func() {
		for update := range set.Watch(true, false) {
			if update.Online {
				// add update.Addr to connection pool
			} else {
				// remove update.Addr from connection pool
			}
		}
	}()
}

func ExampleRegisterWithSet_upgradeDowngrade() {
	set, _ := RegisterWithSet("cluster", ":9099", nil)
	go func() {
		leaders := set.Leaders()
		currentLeader := false
		for leader := range leaders {
			if leader.Addr == set.SelfAddr() {
				currentLeader = true
				// upgrade to leader
			} else if currentLeader == true {
				currentLeader = false
				// downgrade from leader
			}
		}
	}()
	// run server
}

func runEtcdServer(t *testing.T) func() {
	killCh := make(chan struct{})
	doneCh := make(chan struct{})
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	name := "etcd-test." + strconv.Itoa(r.Int())
	dataDir := "/tmp/" + name
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

func runDiscoverdServer(t *testing.T) func() {
	killCh := make(chan struct{})
	doneCh := make(chan struct{})
	go func() {
		cmd := exec.Command("discoverd")
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
	time.Sleep(200 * time.Millisecond)
	return func() {
		close(killCh)
		<-doneCh
	}
}

func setup(t *testing.T) (*Client, func()) {
	killEtcd := runEtcdServer(t)
	killDiscoverd := runDiscoverdServer(t)
	client, err := NewClient()
	if err != nil {
		t.Fatal(err)
	}
	return client, func() {
		client.UnregisterAll()
		killDiscoverd()
		killEtcd()
	}
}

func assert(err error, t *testing.T) error {
	if err != nil {
		t.Fatal("Unexpected error:", err.Error())
	}
	return err
}

func waitUpdates(t *testing.T, set ServiceSet, bringCurrent bool, n int) func() {
	updates := set.Watch(bringCurrent, false)
	return func() {
		defer set.Unwatch(updates)
		for i := 0; i < n; i++ {
			select {
			case u := <-updates:
				t.Logf("update %d: %#v", i, u)
			case <-time.After(3 * time.Second):
				t.Fatalf("Update wait %d timed out", i)
			}
		}
	}
}

func TestBasicRegisterAndServiceSet(t *testing.T) {
	client, cleanup := setup(t)
	defer cleanup()

	serviceName := "basicTest"

	assert(client.RegisterWithAttributes(serviceName, ":1111", map[string]string{"foo": "bar"}), t)
	assert(client.Register(serviceName, ":2222"), t)

	set, err := client.NewServiceSet(serviceName)
	assert(err, t)

	waitUpdates(t, set, true, 2)()
	if len(set.Services()) < 2 {
		t.Fatal("Registered services not online")
	}

	wait := waitUpdates(t, set, false, 1)
	assert(client.Unregister(serviceName, ":2222"), t)
	wait()

	if len(set.Services()) != 1 {
		t.Fatal("Only 1 registered service should be left")
	}
	if set.Services()[0].Attrs["foo"] != "bar" {
		t.Fatal("Attribute not set on service as 'bar'")
	}

	assert(set.Close(), t)
}

func TestNewAttributes(t *testing.T) {
	client, cleanup := setup(t)
	defer cleanup()

	serviceName := "attributeTest"

	set, err := client.NewServiceSet(serviceName)
	assert(err, t)

	assert(client.RegisterWithAttributes(serviceName, ":1111", map[string]string{"foo": "bar"}), t)
	waitUpdates(t, set, true, 1)()
	wait := waitUpdates(t, set, false, 1)
	assert(client.RegisterWithAttributes(serviceName, ":1111", map[string]string{"foo": "baz"}), t)
	wait()

	if s := set.Services()[0]; s.Attrs["foo"] != "baz" {
		t.Fatalf(`Expected attribute set on re-registered service to be "baz", not %q`, s.Attrs["foo"])
	}

	assert(set.Close(), t)
}

func TestFiltering(t *testing.T) {
	client, cleanup := setup(t)
	defer cleanup()

	serviceName := "filterTest"

	set, err := client.NewServiceSet(serviceName)
	assert(err, t)

	watchSet, err := client.NewServiceSet(serviceName)
	assert(err, t)

	assert(client.Register(serviceName, ":1111"), t)
	assert(client.RegisterWithAttributes(serviceName, ":2222", map[string]string{"foo": "qux", "id": "2"}), t)

	set.Filter(map[string]string{"foo": "qux"})
	waitUpdates(t, watchSet, true, 2)
	if len(set.Services()) > 1 {
		t.Fatal("Filter not limiting online services in set")
	}

	assert(client.RegisterWithAttributes(serviceName, ":3333", map[string]string{"foo": "qux", "id": "3"}), t)
	waitUpdates(t, watchSet, true, 3)
	if s := set.Services(); len(s) < 2 {
		t.Fatalf("Filter not letting new matching services in set: %#v", s[0])
	}

	assert(client.RegisterWithAttributes(serviceName, ":4444", map[string]string{"foo": "baz"}), t)
	waitUpdates(t, watchSet, true, 4)
	if len(set.Services()) > 2 {
		t.Fatal("Filter not limiting new unmatching services from set")
	}

	assert(set.Close(), t)
	assert(watchSet.Close(), t)
}

func TestSelecting(t *testing.T) {
	client, cleanup := setup(t)
	defer cleanup()

	serviceName := "selectTest"

	set, err := client.NewServiceSet(serviceName)
	assert(err, t)

	assert(client.Register(serviceName, ":1111"), t)
	assert(client.RegisterWithAttributes(serviceName, ":2222", map[string]string{"foo": "qux", "id": "2"}), t)
	assert(client.RegisterWithAttributes(serviceName, ":3333", map[string]string{"foo": "qux", "id": "3"}), t)

	waitUpdates(t, set, true, 3)()
	if s := set.Select(map[string]string{"id": "3"}); len(s) != 1 {
		t.Fatalf("Expected one service, got: %#v", s)
	}

	assert(set.Close(), t)
}

func TestServices(t *testing.T) {
	client, cleanup := setup(t)
	defer cleanup()

	serviceName := "servicesTest"

	assert(client.Register(serviceName, ":1111"), t)
	assert(client.Register(serviceName, ":2222"), t)

	services, err := client.Services(serviceName, 1)
	assert(err, t)
	if len(services) != 2 {
		t.Fatal("Not all registered services were returned:", services)
	}
}

func TestWatch(t *testing.T) {
	client, cleanup := setup(t)
	defer cleanup()

	serviceName := "watchTest"

	assert(client.Register(serviceName, ":1111"), t)
	assert(client.Register(serviceName, ":2222"), t)

	set, err := client.NewServiceSet(serviceName)
	assert(err, t)

	updates := set.Watch(true, false)
	assert(client.Register(serviceName, ":3333"), t)
	for i := 0; i < 3; i++ {
		var update *agent.ServiceUpdate
		select {
		case update = <-updates:
		case <-time.After(3 * time.Second):
			t.Fatal("Timeout exceeded", i)
		}
		if update.Online != true {
			t.Fatal("Service update of unexected status: ", update, i)
		}
		if update.Name != serviceName {
			t.Fatal("Service update of unexected name: ", update, i)
		}
	}

	assert(set.Close(), t)
}

func TestNoServices(t *testing.T) {
	client, cleanup := setup(t)
	defer cleanup()

	set, err := client.NewServiceSet("nonexistent")
	assert(err, t)

	if len(set.Services()) != 0 {
		t.Fatal("There should be no services")
	}

	assert(set.Close(), t)
}

func TestRegisterWithSet(t *testing.T) {
	client, cleanup := setup(t)
	defer cleanup()

	serviceName := "registerWithSetTest"

	assert(client.Register(serviceName, ":1111"), t)

	set, err := client.RegisterWithSet(serviceName, ":2222", nil)
	assert(err, t)

	if len(set.Services()) != 1 {
		t.Fatal("There should only be one other service")
	}
	if set.Services()[0].Addr != "127.0.0.1:1111" {
		t.Fatal("Set contains the wrong service")
	}

	assert(set.Close(), t)

	services, err := client.Services(serviceName, 1)
	assert(err, t)
	if len(services) != 2 {
		t.Fatal("Not all registered services were returned:", services)
	}
}

func TestServiceAge(t *testing.T) {
	client, cleanup := setup(t)
	defer cleanup()

	serviceName := "ageTest"

	checkOldest := func(addr string) {
		services, err := client.Services(serviceName, 1)
		assert(err, t)
		if services[0].Addr != "127.0.0.1"+addr {
			t.Fatal("Oldest service is not first in Services() slice")
		}
	}

	assert(client.Register(serviceName, ":1111"), t)
	checkOldest(":1111")
	assert(client.Register(serviceName, ":2222"), t)
	checkOldest(":1111")
	assert(client.Register(serviceName, ":3333"), t)
	checkOldest(":1111")
	assert(client.Register(serviceName, ":4444"), t)
	checkOldest(":1111")
	assert(client.Unregister(serviceName, ":1111"), t)
	checkOldest(":2222")

}

func TestLeaderChannel(t *testing.T) {
	client, cleanup := setup(t)
	defer cleanup()

	serviceName := "leadersTest"

	assert(client.Register(serviceName, ":1111"), t)

	set, err := client.NewServiceSet(serviceName)
	assert(err, t)

	leader := make(chan *Service, 3)

	go func() {
		leaders := set.Leaders()
		for {
			leader <- <-leaders
		}
	}()

	if (<-leader).Addr != "127.0.0.1:1111" {
		t.Fatal("Incorrect leader")
	}

	assert(client.Unregister(serviceName, ":1111"), t)

	if (<-leader) != nil {
		t.Fatal("Incorrect leader")
	}

	assert(client.Register(serviceName, ":2222"), t)
	assert(client.Register(serviceName, ":3333"), t)

	if (<-leader).Addr != "127.0.0.1:2222" {
		t.Fatal("Incorrect leader", leader)
	}

	assert(client.Unregister(serviceName, ":2222"), t)

	if (<-leader).Addr != "127.0.0.1:3333" {
		t.Fatal("Incorrect leader")
	}

	assert(set.Close(), t)
}

func TestRegisterWithSetLeaderSelf(t *testing.T) {
	client, cleanup := setup(t)
	defer cleanup()

	serviceName := "registerWithSetLeaderSelfTest"

	assert(client.Register(serviceName, ":1111"), t)

	set, err := client.RegisterWithSet(serviceName, ":2222", nil)
	assert(err, t)

	leader := make(chan *Service, 2)

	go func() {
		leaders := set.Leaders()
		for {
			leader <- <-leaders
		}
	}()

	assert(client.Register(serviceName, ":3333"), t)

	if (<-leader).Addr != "127.0.0.1:1111" {
		t.Fatal("Incorrect leader")
	}

	assert(client.Unregister(serviceName, ":1111"), t)

	if (<-leader).Addr != set.SelfAddr() {
		t.Fatal("Incorrect leader", leader)
	}

	assert(set.Close(), t)

}

func TestRegisterAndStandby(t *testing.T) {
	client, cleanup := setup(t)
	defer cleanup()

	serviceName := "registerAndStandbyTest"

	assert(client.Register(serviceName, ":1111"), t)

	standbyCh, err := client.RegisterAndStandby(serviceName, ":2222", nil)
	assert(err, t)

	assert(client.Register(serviceName, ":3333"), t)
	assert(client.Unregister(serviceName, ":3333"), t)
	assert(client.Unregister(serviceName, ":1111"), t)

	leader := <-standbyCh
	if leader.Addr != "127.0.0.1:2222" {
		t.Fatal("Incorrect leader", leader)
	}

}

func TestUnregisterAll(t *testing.T) {
	client, cleanup := setup(t)
	defer cleanup()

	serviceName := "unregisterAllTest"

	assert(client.Register(serviceName, ":1111"), t)
	assert(client.Register(serviceName, ":2222"), t)
	assert(client.Register(serviceName, ":3333"), t)

	services, err := client.Services(serviceName, 1)
	assert(err, t)
	if len(services) != 3 {
		t.Fatal("Wrong number of services")
	}

	assert(client.UnregisterAll(), t)

	set, err := client.NewServiceSet(serviceName)
	assert(err, t)

	if len(set.Services()) != 0 {
		t.Fatal("There should be no services")
	}

	assert(set.Close(), t)

}

func TestDefaultClient(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	serviceName := "defaultClientTest"

	assert(Register(serviceName, ":1111"), t)
	assert(Register(serviceName, ":2222"), t)
	assert(Register(serviceName, ":3333"), t)

	services, err := Services(serviceName, 1)
	assert(err, t)
	if len(services) != 3 {
		t.Fatal("Wrong number of services")
	}

	assert(UnregisterAll(), t)

	set, err := NewServiceSet(serviceName)
	assert(err, t)

	if len(set.Services()) != 0 {
		t.Fatal("There should be no services")
	}

	assert(set.Close(), t)

}

func TestHeartbeat(t *testing.T) {
	client, cleanup := setup(t)
	defer cleanup()

	serviceName := "heartbeatTest"
	assert(client.Register(serviceName, ":1111"), t)
	time.Sleep(12 * time.Second) // wait for one heartbeat
	services, err := client.Services(serviceName, 1)
	assert(err, t)
	if len(services) != 1 {
		t.Fatal("Missing services")
	}
}
