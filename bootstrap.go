package bootstrap

import (
	"encoding/json"
	"fmt"
	"reflect"
	"time"

	"github.com/flynn/flynn-controller/client"
	ct "github.com/flynn/flynn-controller/types"
	"github.com/flynn/go-discoverd"
	"github.com/flynn/go-flynn/attempt"
	"github.com/flynn/go-flynn/cluster"
)

type State struct {
	StepData  map[string]interface{}
	Providers map[string]*ct.Provider

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
		cc, err := controller.NewClient("discoverd+http://flynn-controller", s.controllerKey)
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
	Timestamp time.Time   `json:"ts"`
}

var Attempts = attempt.Strategy{
	Min:   5,
	Total: 30 * time.Second,
	Delay: 200 * time.Millisecond,
}

func Run(manifest []byte, ch chan<- *StepInfo) (err error) {
	var a StepAction
	defer close(ch)
	defer func() {
		if err != nil {
			ch <- &StepInfo{StepAction: a, State: "error", Error: err.Error(), Timestamp: time.Now().UTC()}
		}
	}()

	// Make sure we are connected to discoverd first
	Attempts.Run(func() error {
		return discoverd.Connect("")
	})

	steps := make([]json.RawMessage, 0)
	if err := json.Unmarshal(manifest, &steps); err != nil {
		return err
	}

	state := &State{
		StepData:  make(map[string]interface{}),
		Providers: make(map[string]*ct.Provider),
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
