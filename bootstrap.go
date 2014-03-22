package bootstrap

import (
	"encoding/json"
	"fmt"
	"reflect"
)

type State struct{}

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

	var state *State
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
