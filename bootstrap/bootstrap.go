package bootstrap

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"time"

	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/pkg/attempt"
	"github.com/flynn/flynn/pkg/cluster"
	"github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/stream"
)

type State struct {
	StepData  map[string]interface{}
	Providers map[string]*ct.Provider
	Singleton bool

	clusterc    *cluster.Client
	controllerc *controller.Client

	controllerKey string
}

func (s *State) ClusterClient() (*cluster.Client, error) {
	if s.clusterc == nil {
		cc, err := cluster.NewClient()
		if err != nil {
			return nil, err
		}
		s.clusterc = cc
	}
	return s.clusterc, nil
}

func (s *State) ControllerClient() (*controller.Client, error) {
	if s.controllerc == nil {
		instances, err := discoverd.GetInstances("flynn-controller", time.Second)
		if err != nil {
			return nil, err
		}
		cc, err := controller.NewClient("http://"+instances[0].Addr, s.controllerKey)
		if err != nil {
			return nil, err
		}
		s.controllerc = cc
	}
	return s.controllerc, nil
}

func (s *State) SetControllerKey(key string) {
	s.controllerKey = key
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

func Run(manifest []byte, ch chan<- *StepInfo, minHosts int) (err error) {
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

	// Make sure we are connected to discoverd first
	discoverdAttempts.Run(func() error {
		return discoverd.DefaultClient.Ping()
	})

	steps := make([]json.RawMessage, 0)
	if err := json.Unmarshal(manifest, &steps); err != nil {
		return err
	}

	state := &State{
		StepData:  make(map[string]interface{}),
		Providers: make(map[string]*ct.Provider),
		Singleton: minHosts == 1,
	}
	if s := os.Getenv("SINGLETON"); s != "" {
		state.Singleton = s == "true"
	}

	a = StepAction{ID: "online-hosts", Action: "check"}
	ch <- &StepInfo{StepAction: a, State: "start", Timestamp: time.Now().UTC()}
	if err := checkOnlineHosts(minHosts, state); err != nil {
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

func checkOnlineHosts(count int, state *State) error {
	var online int
	service := discoverd.NewService("flynn-host")
	updates := make(chan *discoverd.Event)
	var s stream.Stream
	if err := onlineHostAttempts.Run(func() (err error) {
		s, err = service.Watch(updates)
		return
	}); err != nil {
		return err
	}
	defer s.Close()

	timeout := time.After(30 * time.Second)
loop:
	for {
		select {
		case <-updates:
			instances, err := service.Instances()
			if err != nil {
				if e, ok := err.(httphelper.JSONError); ok && e.Code == httphelper.ObjectNotFoundError {
					continue
				}
				return err
			}
			online = len(instances)
			if online >= count {
				break loop
			}
		case <-timeout:
			return fmt.Errorf("timed out waiting for %d hosts to come online (currently %d online)", count, online)
		}
	}

	return onlineHostAttempts.Run(func() error {
		hosts, err := clusterHosts(state)
		if err != nil {
			return err
		}
		online = len(hosts)
		if online < count {
			return fmt.Errorf("expected %d online hosts, got %d", count, online)
		}
		return nil
	})
}

func clusterHosts(state *State) ([]host.Host, error) {
	cc, err := state.ClusterClient()
	if err != nil {
		return nil, err
	}
	return cc.ListHosts()
}
