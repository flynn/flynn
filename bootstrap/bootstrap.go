package bootstrap

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"reflect"
	"sort"
	"text/template"
	"time"

	"github.com/flynn/flynn/bootstrap/discovery"
	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/attempt"
	"github.com/flynn/flynn/pkg/cluster"
	"github.com/flynn/flynn/pkg/random"
)

type State struct {
	StepData   map[string]interface{}
	Providers  map[string]*ct.Provider
	Singleton  bool
	ClusterURL string
	MinHosts   int
	Hosts      []*cluster.Host

	discoverd     *discoverd.Client
	controller    *controller.Client
	controllerKey string
}

func (s *State) ControllerClient() (*controller.Client, error) {
	if s.controller == nil {
		disc, err := s.DiscoverdClient()
		if err != nil {
			return nil, err
		}
		instances, err := disc.Instances("flynn-controller", time.Second)
		if err != nil {
			return nil, err
		}
		cc, err := controller.NewClient("http://"+instances[0].Addr, s.controllerKey)
		if err != nil {
			return nil, err
		}
		s.controller = cc
	}
	return s.controller, nil
}

func (s *State) SetControllerKey(key string) {
	s.controllerKey = key
}

func (s *State) DiscoverdClient() (*discoverd.Client, error) {
	if s.discoverd == nil {
		host, _, err := net.SplitHostPort(s.Hosts[0].Addr())
		if err != nil {
			return nil, err
		}
		s.discoverd = discoverd.NewClientWithURL(fmt.Sprintf("http://%s:1111", host))
	}
	return s.discoverd, nil
}

func (s *State) ShuffledHosts() []*cluster.Host {
	hosts := make([]*cluster.Host, len(s.Hosts))
	copy(hosts, s.Hosts)
	for i := len(hosts) - 1; i > 0; i-- {
		j := random.Math.Intn(i + 1)
		hosts[i], hosts[j] = hosts[j], hosts[i]
	}
	return hosts
}

func (s *State) SortedHostIPs() []string {
	ips := make([]string, len(s.Hosts))
	for i, h := range s.Hosts {
		ips[i], _, _ = net.SplitHostPort(h.Addr())
	}
	sort.Strings(ips)
	return ips
}

type Action interface {
	Run(*State) error
}

var registeredActions = make(map[string]reflect.Type)

func Register(name string, action Action) {
	registeredActions[name] = reflect.Indirect(reflect.ValueOf(action)).Type()
}

type StepAction struct {
	ID     string `json:"id"`
	Action string `json:"action"`
}

type StepInfo struct {
	StepAction
	StepData  interface{} `json:"data,omitempty"`
	State     string      `json:"state"`
	Error     string      `json:"error,omitempty"`
	Err       error       `json:"-"`
	Timestamp time.Time   `json:"ts"`
}

var discoverdAttempts = attempt.Strategy{
	Min:   5,
	Total: 30 * time.Second,
	Delay: 200 * time.Millisecond,
}

func Run(manifest []byte, ch chan<- *StepInfo, clusterURL string, ips []string, minHosts int) (err error) {
	var a StepAction
	defer close(ch)
	defer func() {
		if err != nil {
			ch <- &StepInfo{StepAction: a, State: "error", Error: err.Error(), Err: err, Timestamp: time.Now().UTC()}
		}
	}()

	if minHosts == 2 {
		return errors.New("the minimum number of hosts for a multi-node cluster is 3, min-hosts=2 is invalid")
	}

	steps := make([]json.RawMessage, 0)
	if err := json.Unmarshal(manifest, &steps); err != nil {
		return err
	}

	state := &State{
		StepData:   make(map[string]interface{}),
		Providers:  make(map[string]*ct.Provider),
		Singleton:  minHosts == 1,
		MinHosts:   minHosts,
		ClusterURL: clusterURL,
	}
	if s := os.Getenv("SINGLETON"); s != "" {
		state.Singleton = s == "true"
	}
	var hostURLs []string
	if len(ips) > 0 {
		hostURLs = make([]string, len(ips))
		for i, ip := range ips {
			hostURLs[i] = fmt.Sprintf("http://%s:1113", ip)
		}
	}

	a = StepAction{ID: "online-hosts", Action: "check"}
	ch <- &StepInfo{StepAction: a, State: "start", Timestamp: time.Now().UTC()}
	if err := checkOnlineHosts(minHosts, state, hostURLs); err != nil {
		return err
	}

	for _, s := range steps {
		if err := json.Unmarshal(s, &a); err != nil {
			return err
		}
		actionType, ok := registeredActions[a.Action]
		if !ok {
			return fmt.Errorf("bootstrap: unknown action %q", a.Action)
		}
		action := reflect.New(actionType).Interface().(Action)

		if err := json.Unmarshal(s, action); err != nil {
			return err
		}

		ch <- &StepInfo{StepAction: a, State: "start", Timestamp: time.Now().UTC()}

		if err := action.Run(state); err != nil {
			return err
		}

		si := &StepInfo{StepAction: a, State: "done", Timestamp: time.Now().UTC()}
		if data, ok := state.StepData[a.ID]; ok {
			si.StepData = data
		}
		ch <- si
	}

	return nil
}

var onlineHostAttempts = attempt.Strategy{
	Min:   5,
	Total: 5 * time.Second,
	Delay: 200 * time.Millisecond,
}

func checkOnlineHosts(expected int, state *State, urls []string) error {
	if len(urls) == 0 {
		urls = []string{"http://127.0.0.1:1113"}
	}
	timeout := time.After(30 * time.Second)
	for {
		if state.ClusterURL != "" {
			instances, err := discovery.GetCluster(state.ClusterURL)
			if err != nil {
				return fmt.Errorf("error discovering cluster: %s", err)
			}
			urls = make([]string, len(instances))
			for i, inst := range instances {
				urls[i] = inst.URL
			}
		}

		known := len(urls)
		online := 0
		if known >= expected {
			state.Hosts = make([]*cluster.Host, 0, known)
			for _, url := range urls {
				h := cluster.NewHost("", url, nil)
				status, err := h.GetStatus()
				if err != nil {
					continue
				}
				online++
				state.Hosts = append(state.Hosts, cluster.NewHost(status.ID, status.URL, nil))
			}
			if online >= expected {
				break
			}
		}

		select {
		case <-timeout:
			return fmt.Errorf("timed out waiting for %d hosts to come online (currently %d online)", expected, online)
		default:
			time.Sleep(time.Second)
		}
	}
	return nil
}

func interpolate(s *State, arg string) string {
	t, err := template.New("arg").Funcs(template.FuncMap{
		"getenv": os.Getenv,
		"md5sum": md5sum,
	}).Parse(arg)
	if err != nil {
		log.Printf("Ignoring error parsing %q as template: %s", arg, err)
		return arg
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, s); err != nil {
		log.Printf("Ignoring error executing %q as template: %s", arg, err)
		return arg
	}
	return buf.String()
}

func md5sum(s string) string {
	h := md5.Sum([]byte(s))
	return hex.EncodeToString(h[:])
}
