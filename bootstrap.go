package bootstrap

import (
	"encoding/json"
	"fmt"
	"log"
	"reflect"

	"github.com/flynn/flynn-controller/client"
	ct "github.com/flynn/flynn-controller/types"
	"github.com/flynn/go-flynn/cluster"
)

type State struct {
	StepData  map[string]interface{}
	Providers map[string]*ct.Provider

	clusterc    *cluster.Client
	controllerc *controller.Client
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
		cc, err := controller.NewClient("discoverd+http://flynn-controller")
		if err != nil {
			return nil, err
		}
		s.controllerc = cc
	}
	return s.controllerc, nil
}

type Action interface {
	Run(*State) error
}

type CleanAction interface {
	Cleanup(*State) error
}

var registeredActions = make(map[string]reflect.Type)

func Register(name string, action Action) {
	registeredActions[name] = reflect.Indirect(reflect.ValueOf(action)).Type()
}

type stepAction struct {
	ID     string `json:"id"`
	Action string `json:"action"`
}

type CleanupError struct {
	error
	CleanupErrors []error
}

func Run(manifest []byte) error {
	steps := make([]json.RawMessage, 0)
	if err := json.Unmarshal(manifest, &steps); err != nil {
		return err
	}

	state := &State{
		StepData:  make(map[string]interface{}),
		Providers: make(map[string]*ct.Provider),
	}
	actions := make([]Action, 0, len(steps))
	cleanup := func(err error) error {
		errors := make([]error, 0, len(steps))
		for i := len(actions) - 1; i >= 0; i-- {
			if ca, ok := actions[i].(CleanAction); ok {
				err := ca.Cleanup(state)
				if err != nil {
					errors = append(errors, err)
				}
			}
		}
		if len(errors) > 0 {
			return CleanupError{err, errors}
		}
		return err
	}

	for _, s := range steps {
		var a stepAction
		if err := json.Unmarshal(s, &a); err != nil {
			return cleanup(err)
		}
		actionType, ok := registeredActions[a.Action]
		if !ok {
			return cleanup(fmt.Errorf("bootstrap: unknown action %q", a.Action))
		}
		action := reflect.New(actionType).Interface().(Action)

		if err := json.Unmarshal(s, action); err != nil {
			return cleanup(err)
		}
		log.Printf("%s %s", a.Action, a.ID)
		if err := action.Run(state); err != nil {
			return cleanup(err)
		}
	}

	return nil
}
