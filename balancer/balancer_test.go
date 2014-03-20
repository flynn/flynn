package balancer

import (
	"github.com/flynn/discoverd/agent"
	"github.com/flynn/go-discoverd"

	"math/rand"
	"testing"
)

// implements discoverd.ServiceSet
type TestSet struct {
	services []*discoverd.Service
}

func (test *TestSet) SelfAddr() string { return "127.0.0.1" }

func (test *TestSet) Leader() *discoverd.Service { return test.services[0] }

func (test *TestSet) Leaders() chan *discoverd.Service { return nil }

func (test *TestSet) Services() []*discoverd.Service { return test.services }

func (test *TestSet) Addrs() []string { return []string{} }

func (test *TestSet) Select(attrs map[string]string) []*discoverd.Service { return test.services }

func (test *TestSet) Filter(attrs map[string]string) {}

func (test *TestSet) Watch(bringCurrent bool, fireOnce bool) chan *agent.ServiceUpdate { return nil }

func (test *TestSet) Unwatch(chan *agent.ServiceUpdate) {}

func (test *TestSet) Close() error { return nil }

func NewTestSet() discoverd.ServiceSet {
	return &TestSet{
		[]*discoverd.Service{
			&discoverd.Service{Host: "flying-manta-10.flynn.io"},
			&discoverd.Service{Host: "singing-shark-82.flynn.io"},
			&discoverd.Service{Host: "passionate-sheep-19.flynn.io"},
		},
	}
}

func assertHost(balancer LoadBalancer, expected string, t *testing.T) {
	service, err := balancer.Next()
	if err != nil {
		t.Fatal("Did not expect balancer to yield an error: ", err)
	}
	if service.Host != expected {
		t.Fatalf("Expected %s, got %s", expected, service.Host)
	}
}

func TestRandom(t *testing.T) {
	balancer := Random(NewTestSet(), rand.NewSource(100))

	assertHost(balancer, "singing-shark-82.flynn.io", t)
	assertHost(balancer, "passionate-sheep-19.flynn.io", t)
	assertHost(balancer, "singing-shark-82.flynn.io", t)
	assertHost(balancer, "flying-manta-10.flynn.io", t)
}

func TestRoundRobin(t *testing.T) {
	balancer := RoundRobin(NewTestSet())

	assertHost(balancer, "flying-manta-10.flynn.io", t)
	assertHost(balancer, "singing-shark-82.flynn.io", t)
	assertHost(balancer, "passionate-sheep-19.flynn.io", t)
	assertHost(balancer, "flying-manta-10.flynn.io", t)
}

func TestRandomEmptySet(t *testing.T) {
	set := &TestSet{}
	random := Random(set, rand.NewSource(100))
	if _, err := random.Next(); err != ErrNoServices {
		t.Fatal("Expected to get an error back from Random balancer when no services available")
	}
}

func TestRoundRobinEmptySet(t *testing.T) {
	set := &TestSet{}
	roundRobin := RoundRobin(set)
	if _, err := roundRobin.Next(); err != ErrNoServices {
		t.Fatal("Expected to get nil back from RoundRobin balancer when no services available")
	}
}
