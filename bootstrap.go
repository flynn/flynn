package bootstrap

import (
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/flynn/go-flynn/cluster"
)

type State struct {
	StepData map[string]interface{}

	cc *cluster.Client
}

func (s *State) ClusterClient() (*cluster.Client, error) {
	if s.cc == nil {
		cc, err := cluster.NewClient()
		if err != nil {
			return nil, err
		}
		s.cc = cc
		return cc, nil
	}
	return s.cc, nil
}

type Action interface {
	Run(*State) error
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

	state := &State{StepData: make(map[string]interface{})}
	actions := make([]Action, 0, len(steps))
	cleanup := func(err error) error {
		errors := make([]error, 0, len(steps))
		for i := len(actions) - 1; i >= 0; i-- {
			err := actions[i].Cleanup(state)
			if err != nil {
				errors = append(errors, err)
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
		if err := action.Run(state); err != nil {
			return cleanup(err)
		}
	}

	return nil
}
