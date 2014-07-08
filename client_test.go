package discoverd_test

import (
	"bufio"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"math/rand"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/coreos/go-etcd/etcd"
	"github.com/flynn/discoverd/agent"
	"github.com/flynn/go-discoverd"
	"github.com/flynn/go-flynn/attempt"
)

func ExampleRegisterAndStandby_standby() {
	standbyCh, err := discoverd.RegisterAndStandby("sampi", ":9099", nil)
	if err != nil {
		panic(err)
	}
	<-standbyCh
	// run server
}

func ExampleRegisterAndStandby_upgrade() {
	standbyCh, err := discoverd.RegisterAndStandby("sampi", ":9099", nil)
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
	set, err := discoverd.NewServiceSet("sampi")
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
	set, err := discoverd.NewServiceSet("app")
	if err != nil {
		panic(err)
	}
	go func() {
		for update := range set.Watch(true) {
			if update.Online {
				// add update.Addr to connection pool
			} else {
				// remove update.Addr from connection pool
			}
		}
	}()
}

func ExampleRegisterWithSet_upgradeDowngrade() {
	set, _ := discoverd.RegisterWithSet("cluster", ":9099", nil)
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

var Attempts = attempt.Strategy{
	Min:   5,
	Total: 5 * time.Second,
	Delay: 200 * time.Millisecond,
}

func runEtcdServer(t *testing.T) func() {
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

func runDiscoverdServer(t *testing.T, addr string) func() {
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

func bootDiscoverd(t *testing.T, addr string) (*discoverd.Client, func()) {
	if addr == "" {
		addr = "127.0.0.1:1111"
	}
	killDiscoverd := runDiscoverdServer(t, addr)

	var client *discoverd.Client
	err := Attempts.Run(func() (err error) {
		client, err = discoverd.NewClientWithAddr(addr)
		return
	})
	if err != nil {
		t.Fatalf("Failed to connect to discoverd: %q", err)
	}
	return client, killDiscoverd
}

func setup(t *testing.T) (*discoverd.Client, func()) {
	killEtcd := runEtcdServer(t)
	client, killDiscoverd := bootDiscoverd(t, "")

	return client, func() {
		client.UnregisterAll()
		client.Close()
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

func waitUpdates(t *testing.T, set discoverd.ServiceSet, bringCurrent bool, n int) func() {
	updates := set.Watch(bringCurrent)
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

func checkUpdates(updates chan *agent.ServiceUpdate, expected []*agent.ServiceUpdate) error {
	for _, u := range expected {
		if err := checkUpdate(updates, u); err != nil {
			return err
		}
	}
	return nil
}

func checkUpdate(updates chan *agent.ServiceUpdate, expected *agent.ServiceUpdate) error {
	select {
	case u := <-updates:
		if !updatesEqual(u, expected) {
			return fmt.Errorf("Expected update: %v, got %v", expected, u)
		}
		return nil
	case <-time.After(3 * time.Second):
		return fmt.Errorf("Timed out waiting for update: %v", expected)
	}
}

func updatesEqual(a, b *agent.ServiceUpdate) bool {
	if a.Name != b.Name || a.Addr != b.Addr || a.Online != b.Online {
		return false
	}
	for key, val := range a.Attrs {
		v, exists := b.Attrs[key]
		if !exists || v != val {
			return false
		}
	}
	return true
}

func checkServices(t *testing.T, actual []*discoverd.Service, expected []*discoverd.Service) {
	for _, service := range actual {
		if !includesService(expected, service) {
			t.Fatalf("Expected %#v to include %v", actual, service)
		}
	}
}

func includesService(services []*discoverd.Service, service *discoverd.Service) bool {
	for _, s := range services {
		if servicesEqual(s, service) {
			return true
		}
	}
	return false
}

func servicesEqual(a, b *discoverd.Service) bool {
	if a.Name != b.Name || a.Host != b.Host || a.Port != b.Port || a.Addr != b.Addr {
		return false
	}
	for key, val := range a.Attrs {
		v, exists := b.Attrs[key]
		if !exists || v != val {
			return false
		}
	}
	return true
}

func waitForConnStatus(t *testing.T, ch chan discoverd.ConnEvent, status discoverd.ConnStatus) {
	failureThreshold := 5
	for {
		select {
		case e := <-ch:
			if status == discoverd.ConnStatusConnected && e.Status == discoverd.ConnStatusConnectFailed {
				failureThreshold--
				if failureThreshold == 0 {
					t.Fatalf("Too many failures waiting for reconnection")
				}
				continue
			}
			if e.Status != status {
				t.Fatalf("Expected connection status %d, got %d", status, e.Status)
			}
			return
		case <-time.After(3 * time.Second):
			t.Fatalf("Timed out waiting for connection status: %d", status)
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
	if len(set.Services()) < 2 || len(set.Addrs()) < 2 {
		t.Fatal("Registered services not online")
	}

	wait := waitUpdates(t, set, false, 1)
	assert(client.Unregister(serviceName, ":2222"), t)
	wait()

	if len(set.Services()) != 1 || len(set.Addrs()) != 1 {
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
	waitUpdates(t, watchSet, true, 2)()
	if len(set.Services()) > 1 {
		t.Fatal("Filter not limiting online services in set")
	}

	assert(client.RegisterWithAttributes(serviceName, ":3333", map[string]string{"foo": "qux", "id": "3"}), t)
	waitUpdates(t, set, true, 2)()
	if s := set.Services(); len(s) < 2 {
		t.Fatalf("Filter not letting new matching services in set: %#v", s[0])
	}

	assert(client.RegisterWithAttributes(serviceName, ":4444", map[string]string{"foo": "baz"}), t)
	waitUpdates(t, watchSet, true, 4)()
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

func TestReconnect(t *testing.T) {
	clientA, cleanup := setup(t)
	defer cleanup()

	clientB, killDiscoverd := bootDiscoverd(t, "127.0.0.1:1112")
	defer func() {
		clientB.UnregisterAll()
		clientB.Close()
		killDiscoverd()
	}()

	service1 := "serviceReconnect-1"
	service2 := "serviceReconnect-2"

	assert(clientA.Register(service1, ":1111"), t)
	assert(clientA.Register(service1, ":2222"), t)
	assert(clientA.Register(service2, ":1111"), t)
	assert(clientA.Register(service2, ":2222"), t)

	set1, err := clientB.NewServiceSet(service1)
	assert(err, t)
	waitUpdates(t, set1, true, 2)()

	set2, err := clientB.NewServiceSet(service2)
	assert(err, t)
	waitUpdates(t, set2, true, 2)()

	updates1 := set1.Watch(false)
	updates2 := set2.Watch(false)

	reconnCh := clientB.WatchReconnects()
	defer clientB.UnwatchReconnects(reconnCh)

	killDiscoverd()

	waitForConnStatus(t, reconnCh, discoverd.ConnStatusDisconnected)

	if err := clientB.Register(service1, ":3333"); err != discoverd.ErrDisconnected {
		t.Fatal("expected ErrDisconnected from clientB, got:", err)
	}

	if _, err := clientB.Services(service2, 1); err != discoverd.ErrDisconnected {
		t.Fatal("expected ErrDisconnected from clientB, got:", err)
	}

	assert(clientA.RegisterWithAttributes(service1, ":1111", map[string]string{"foo": "bar"}), t)
	assert(clientA.Unregister(service1, ":2222"), t)
	assert(clientA.Unregister(service2, ":1111"), t)
	assert(clientA.Register(service2, ":3333"), t)

	killDiscoverd = runDiscoverdServer(t, "127.0.0.1:1112")

	waitForConnStatus(t, reconnCh, discoverd.ConnStatusConnected)

	// use goroutines to check for updates so slow watchers don't block the rpc stream
	updateErrors := make(chan error)
	go func() {
		updateErrors <- checkUpdates(updates1, []*agent.ServiceUpdate{
			{
				Name:   service1,
				Addr:   "127.0.0.1:1111",
				Online: true,
				Attrs:  map[string]string{"foo": "bar"},
			},
			{
				Name:   service1,
				Addr:   "127.0.0.1:2222",
				Online: false,
			},
		})
	}()
	go func() {
		updateErrors <- checkUpdates(updates2, []*agent.ServiceUpdate{
			{
				Name:   service2,
				Addr:   "127.0.0.1:3333",
				Online: true,
			},
			{
				Name:   service2,
				Addr:   "127.0.0.1:2222",
				Online: true,
			},
			{
				Name:   service2,
				Addr:   "127.0.0.1:1111",
				Online: false,
			},
		})
	}()

	var updateError error
	for i := 0; i < 2; i++ {
		if err := <-updateErrors; err != nil && updateError == nil {
			updateError = err
		}
	}
	if updateError != nil {
		t.Fatal(updateError)
	}

	assert(clientA.Register(service1, ":3333"), t)

	if err := checkUpdate(updates1, &agent.ServiceUpdate{
		Name:   service1,
		Addr:   "127.0.0.1:3333",
		Online: true,
	}); err != nil {
		t.Fatal(err)
	}

	// wait for one heartbeat
	time.Sleep(agent.HeartbeatIntervalSecs*time.Second + time.Second)

	checkServices(t, set1.Services(), []*discoverd.Service{
		{Name: service1, Host: "127.0.0.1", Port: "1111", Addr: "127.0.0.1:1111", Attrs: map[string]string{"foo": "bar"}},
		{Name: service1, Host: "127.0.0.1", Port: "3333", Addr: "127.0.0.1:3333"},
	})

	checkServices(t, set2.Services(), []*discoverd.Service{
		{Name: service2, Host: "127.0.0.1", Port: "2222", Addr: "127.0.0.1:2222"},
		{Name: service2, Host: "127.0.0.1", Port: "3333", Addr: "127.0.0.1:3333"},
	})
}

func TestWatch(t *testing.T) {
	client, cleanup := setup(t)
	defer cleanup()

	serviceName := "watchTest"

	assert(client.Register(serviceName, ":1111"), t)
	assert(client.Register(serviceName, ":2222"), t)

	set, err := client.NewServiceSet(serviceName)
	assert(err, t)

	updates := set.Watch(true)
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

	leader := make(chan *discoverd.Service, 3)

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

	leader := make(chan *discoverd.Service, 2)

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

	assert(discoverd.Register(serviceName, ":1111"), t)
	assert(discoverd.Register(serviceName, ":2222"), t)
	assert(discoverd.Register(serviceName, ":3333"), t)

	services, err := discoverd.Services(serviceName, 1)
	assert(err, t)
	if len(services) != 3 {
		t.Fatal("Wrong number of services")
	}

	assert(discoverd.UnregisterAll(), t)

	set, err := discoverd.NewServiceSet(serviceName)
	assert(err, t)

	if len(set.Services()) != 0 {
		t.Fatal("There should be no services")
	}

	assert(set.Close(), t)
	discoverd.DefaultClient.Close()
	discoverd.DefaultClient = nil
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
