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
	"github.com/flynn/flynn/pkg/cluster"
	"github.com/flynn/flynn/pkg/random"
)

type State struct {
	StepData    map[string]interface{}
	Providers   map[string]*ct.Provider
	Singleton   bool
	ClusterURL  string
	MinHosts    int
	Hosts       []*cluster.Host
	HostTimeout time.Duration

	discoverd     *discoverd.Client
	controller    controller.Client
	controllerKey string
}

func (s *State) ControllerClient() (controller.Client, error) {
	if s.controller == nil {
		disc, err := s.DiscoverdClient()
		if err != nil {
			return nil, err
		}
		instances, err := disc.Instances("controller", time.Second)
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

type StepMeta struct {
	ID     string `json:"id"`
	Action string `json:"action"`
}

type StepInfo struct {
	StepMeta
	StepData  interface{} `json:"data,omitempty"`
	State     string      `json:"state"`
	Error     string      `json:"error,omitempty"`
	Err       error       `json:"-"`
	Timestamp time.Time   `json:"ts"`
}

type Step struct {
	StepMeta
	Action
}

type Config struct {
	ClusterURL string
	IPs        []string
	MinHosts   int
	Timeout    int
	Singleton  bool
}

type Manifest []Step

func stepError(ch chan<- *StepInfo, meta StepMeta, err error) {
	ch <- &StepInfo{StepMeta: meta, State: "error", Error: err.Error(), Err: err, Timestamp: time.Now().UTC()}
}

func (m Manifest) Run(ch chan<- *StepInfo, cfg Config) (state *State, err error) {
	if cfg.MinHosts == 2 {
		return nil, errors.New("the minimum number of hosts for a multi-node cluster is 3, min-hosts=2 is invalid")
	}

	state = &State{
		StepData:    make(map[string]interface{}),
		Providers:   make(map[string]*ct.Provider),
		Singleton:   cfg.Singleton,
		MinHosts:    cfg.MinHosts,
		ClusterURL:  cfg.ClusterURL,
		HostTimeout: time.Duration(cfg.Timeout) * time.Second,
	}

	var hostURLs []string
	if len(cfg.IPs) > 0 {
		hostURLs = make([]string, len(cfg.IPs))
		for i, ip := range cfg.IPs {
			hostURLs[i] = fmt.Sprintf("http://%s:1113", ip)
		}
	}

	meta := StepMeta{ID: "online-hosts", Action: "check"}
	ch <- &StepInfo{StepMeta: meta, State: "start", Timestamp: time.Now().UTC()}
	if err := checkOnlineHosts(cfg.MinHosts, state, hostURLs, state.HostTimeout); err != nil {
		stepError(ch, meta, err)
		return nil, err
	}
	ch <- &StepInfo{StepMeta: meta, State: "done", Timestamp: time.Now().UTC()}

	return m.RunWithState(ch, state)
}

func (m Manifest) RunWithState(ch chan<- *StepInfo, state *State) (*State, error) {
	for _, s := range m {
		ch <- &StepInfo{StepMeta: s.StepMeta, State: "start", Timestamp: time.Now().UTC()}

		if err := s.Run(state); err != nil {
			stepError(ch, s.StepMeta, err)
			return nil, err
		}

		si := &StepInfo{StepMeta: s.StepMeta, State: "done", Timestamp: time.Now().UTC()}
		if data, ok := state.StepData[s.StepMeta.ID]; ok {
			si.StepData = data
		}
		ch <- si
	}
	return state, nil
}

func UnmarshalManifest(manifestData []byte, only []string) (Manifest, error) {
	var manifest Manifest
	var steps []json.RawMessage
	if err := json.Unmarshal(manifestData, &steps); err != nil {
		return nil, err
	}

	skipStep := func(step Step) bool {
		if len(only) == 0 {
			return false
		}
		for _, id := range only {
			if id == step.StepMeta.ID {
				return false
			}
		}
		return true
	}

	for _, s := range steps {
		var step Step
		if err := json.Unmarshal(s, &step.StepMeta); err != nil {
			return nil, err
		}
		if skipStep(step) {
			continue
		}
		actionType, ok := registeredActions[step.StepMeta.Action]
		if !ok {
			return manifest, fmt.Errorf("bootstrap: unknown action %q", step.StepMeta.Action)
		}
		step.Action = reflect.New(actionType).Interface().(Action)

		if err := json.Unmarshal(s, &step.Action); err != nil {
			return nil, err
		}
		manifest = append(manifest, step)
	}
	return manifest, nil
}

func Run(manifestData []byte, ch chan<- *StepInfo, cfg Config, only []string) error {
	defer close(ch)
	manifest, err := UnmarshalManifest(manifestData, only)
	if err != nil {
		return err
	}

	cfg.Singleton = cfg.MinHosts == 1
	if s := os.Getenv("SINGLETON"); s != "" {
		cfg.Singleton = s == "true"
	}

	_, err = manifest.Run(ch, cfg)
	return err
}

func checkOnlineHosts(expected int, state *State, urls []string, timeout time.Duration) error {
	if len(urls) == 0 {
		urls = []string{"http://127.0.0.1:1113"}
	}
	t := time.After(timeout)
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
		remaining := make(map[string]struct{}, known)
		online := 0
		if known >= expected {
			for _, url := range urls {
				remaining[url] = struct{}{}
			}
			state.Hosts = make([]*cluster.Host, 0, known)
			for _, url := range urls {
				h := cluster.NewHost("", url, nil, nil)
				status, err := h.GetStatus()
				if err != nil {
					continue
				}
				delete(remaining, url)
				online++
				state.Hosts = append(state.Hosts, cluster.NewHost(status.ID, status.URL, nil, nil))
			}
			if online >= expected {
				break
			}
		}

		select {
		case <-t:
			msg := fmt.Sprintf("timed out waiting for %d hosts to come online (currently %d online)\n\n", expected, online)
			msg += "The following hosts were discovered but remained unreachable:\n"
			for url := range remaining {
				msg += "\n" + url + "\n"
			}
			msg += "\n"
			return fmt.Errorf(msg)
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
