package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"net"
	"net/http"
	"os"
	"sync/atomic"
	"time"

	"github.com/flynn/flynn/discoverd/cache"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/status"
)

func main() {
	log.Fatal(http.ListenAndServe(":"+os.Getenv("PORT"), status.Handler(GetStatus)))
}

var httpClient = &http.Client{Timeout: 2 * time.Second}

type ReqFn func() (*http.Request, error)

type Service struct {
	Name  string
	ReqFn func() (*http.Request, error)
}

func (s Service) Status() status.Status {
	req, err := s.ReqFn()
	if err != nil {
		return status.Unhealthy
	}
	res, err := httpClient.Do(req)
	if err != nil {
		return status.Unhealthy
	}
	defer res.Body.Close()

	var data struct {
		Data status.Status
	}
	if err := json.NewDecoder(res.Body).Decode(&data); err != nil {
		return status.Unhealthy
	}
	return data.Data
}

var services = []Service{
	{Name: "blobstore"},
	{
		Name: "controller",
		ReqFn: func() ReqFn {
			instances, err := discoverd.GetInstances("controller", 1*time.Second)
			if err != nil {
				log.Fatalf("error discovering controller: %s", err)
			}
			key := instances[0].Meta["AUTH_KEY"]
			fn := RandomReqFn("controller")
			return func() (*http.Request, error) {
				req, err := fn()
				if err != nil {
					return nil, err
				}
				req.SetBasicAuth("", key)
				return req, nil
			}
		}(),
	},
	{Name: "controller-scheduler", ReqFn: LeaderReqFn("controller-scheduler", "")},
	{Name: "controller-worker"},
	{Name: "dashboard", ReqFn: RandomReqFn("dashboard-web")},
	{Name: "discoverd"},
	{Name: "flannel"},
	{Name: "gitreceive", ReqFn: RandomReqFn("gitreceive-http")},
	{Name: "logaggregator", ReqFn: LeaderReqFn("logaggregator", "80")},
	{Name: "postgres", ReqFn: LeaderReqFn("postgres", "5433")},
	{Name: "router", ReqFn: RandomReqFn("router-api")},
}

func RandomReqFn(name string) ReqFn {
	sc, err := cache.New(discoverd.NewService(name))
	if err != nil {
		log.Fatalf("error creating %s cache: %s", name, err)
	}
	return func() (*http.Request, error) {
		addrs := sc.Addrs()
		if len(addrs) == 0 {
			return nil, errors.New("no service instances")
		}
		return http.NewRequest("GET", fmt.Sprintf("http://%s%s", addrs[rand.Intn(len(addrs))], status.Path), nil)
	}
}

func LeaderReqFn(name, port string) ReqFn {
	events := make(chan *discoverd.Event)
	if _, err := discoverd.NewService(name).Watch(events); err != nil {
		log.Fatalf("error creating %s cache: %s", name, err)
	}
	var leader atomic.Value // addr string
	leader.Store("")
	go func() {
		for e := range events {
			if e.Kind != discoverd.EventKindLeader || e.Instance == nil {
				continue
			}
			leader.Store(e.Instance.Addr)
		}
	}()
	return func() (*http.Request, error) {
		addr := leader.Load().(string)
		if addr == "" {
			return nil, errors.New("no leader")
		}
		if port != "" {
			host, _, _ := net.SplitHostPort(addr)
			addr = net.JoinHostPort(host, port)
		}
		return http.NewRequest("GET", fmt.Sprintf("http://%s%s", addr, status.Path), nil)
	}
}

func init() {
	for i, s := range services {
		if s.ReqFn != nil {
			continue
		}
		services[i].ReqFn = RandomReqFn(s.Name)
	}
}

type ServiceStatus struct {
	Name   string
	Status status.Status
}

func GetStatus() status.Status {
	results := make(chan ServiceStatus)
	for _, s := range services {
		go func(s Service) {
			results <- ServiceStatus{s.Name, s.Status()}
		}(s)
	}

	data := make(map[string]status.Status, len(services))
	healthy := true
	for i := 0; i < len(services); i++ {
		res := <-results
		data[res.Name] = res.Status
		if res.Status.Status != status.CodeHealthy {
			healthy = false
		}
	}

	s, _ := status.New(healthy, data)
	return s
}
