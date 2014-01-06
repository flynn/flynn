package discoverd

import (
	"bufio"
	"log"
	"math/rand"
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"

	"github.com/flynn/discoverd/agent"
)

func runEtcdServer() func() {
	killCh := make(chan struct{})
	doneCh := make(chan struct{})
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	name := "etcd-test." + strconv.Itoa(r.Int())
	dataDir := "/tmp/" + name
	go func() {
		cmd := exec.Command("etcd", "-name", name, "-data-dir", dataDir)
		if err := cmd.Start(); err != nil {
			panic(err)
		}
		cmdDone := make(chan error)
		go func() {
			cmdDone <- cmd.Wait()
		}()
		select {
		case <-killCh:
			if err := cmd.Process.Kill(); err != nil {
				panic(err)
			}
			<-cmdDone
		case err := <-cmdDone:
			panic(err)
		}
		if err := os.RemoveAll(dataDir); err != nil {
			panic(err)
		}
		doneCh <- struct{}{}
	}()
	return func() {
		close(killCh)
		<-doneCh
	}
}

func runDiscoverdServer() func() {
	killCh := make(chan struct{})
	doneCh := make(chan struct{})
	go func() {
		cmd := exec.Command("discoverd")
		stderr, _ := cmd.StderrPipe()
		if err := cmd.Start(); err != nil {
			panic(err)
		}
		if os.Getenv("DEBUG") != "" {
			go func() {
				scanner := bufio.NewScanner(stderr)
				for scanner.Scan() {
					log.Println("discoverd:", scanner.Text())
				}
			}()
		}
		cmdDone := make(chan error)
		go func() {
			cmdDone <- cmd.Wait()
		}()
		select {
		case <-killCh:
			if err := cmd.Process.Kill(); err != nil {
				panic(err)
			}
			<-cmdDone
		case err := <-cmdDone:
			panic(err)
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
	killEtcd := runEtcdServer()
	killDiscoverd := runDiscoverdServer()
	client, err := NewClient()
	if err != nil {
		t.Fatal(err)
	}
	return client, func() {
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

func TestBasicRegisterAndServiceSet(t *testing.T) {
	client, cleanup := setup(t)
	defer cleanup()

	serviceName := "basicTest"

	assert(client.RegisterWithAttributes(serviceName, ":1111", map[string]string{"foo": "bar"}), t)
	assert(client.Register(serviceName, ":2222"), t)

	set, err := client.NewServiceSet(serviceName)
	assert(err, t)

	if len(set.Services()) < 2 {
		t.Fatal("Registered services not online")
	}

	assert(client.Unregister(serviceName, ":2222"), t)
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
	assert(client.RegisterWithAttributes(serviceName, ":1111", map[string]string{"foo": "baz"}), t)

	<-set.Watch(true, true)
	if set.Services()[0].Attrs["foo"] != "baz" {
		t.Fatal("Attribute not set on re-registered service as 'baz'")
	}

	assert(set.Close(), t)
}

func TestFiltering(t *testing.T) {
	client, cleanup := setup(t)
	defer cleanup()

	serviceName := "filterTest"

	set, err := client.NewServiceSet(serviceName)
	assert(err, t)

	assert(client.Register(serviceName, ":1111"), t)
	assert(client.RegisterWithAttributes(serviceName, ":2222", map[string]string{"foo": "qux", "id": "2"}), t)

	set.Filter(map[string]string{"foo": "qux"})
	if len(set.Services()) > 1 {
		t.Fatal("Filter not limiting online services in set")
	}

	assert(client.RegisterWithAttributes(serviceName, ":3333", map[string]string{"foo": "qux", "id": "3"}), t)
	if len(set.Services()) < 2 {
		t.Fatal("Filter not letting new matching services in set")
	}

	assert(client.RegisterWithAttributes(serviceName, ":4444", map[string]string{"foo": "baz"}), t)
	if len(set.Services()) > 2 {
		t.Fatal("Filter not limiting new unmatching services from set")
	}

	assert(set.Close(), t)
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

	if len(set.Select(map[string]string{"id": "3"})) != 1 {
		t.Fatal("Select not returning proper services")
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
			t.Fatal("Timeout exceeded")
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
	if set.Services()[0].Addr != ":1111" {
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
		if services[0].Addr != addr {
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

	var leader *Service

	go func() {
		leaders := set.Leaders()
		for {
			leader = <-leaders
		}
	}()

	assert(client.Register(serviceName, ":2222"), t)

	if leader.Addr != ":1111" {
		t.Fatal("Incorrect leader")
	}

	assert(client.Register(serviceName, ":3333"), t)
	assert(client.Unregister(serviceName, ":1111"), t)

	if leader.Addr != ":2222" {
		t.Fatal("Incorrect leader", leader)
	}

	assert(client.Unregister(serviceName, ":2222"), t)

	if leader.Addr != ":3333" {
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

	var leader *Service

	go func() {
		leaders := set.Leaders()
		for {
			leader = <-leaders
		}
	}()

	assert(client.Register(serviceName, ":3333"), t)

	if leader.Addr != ":1111" {
		t.Fatal("Incorrect leader")
	}

	assert(client.Unregister(serviceName, ":1111"), t)

	if leader.Addr != set.SelfAddr {
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
	if leader.Addr != ":2222" {
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

func TestDefaulClient(t *testing.T) {
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
