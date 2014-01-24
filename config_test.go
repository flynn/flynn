package main

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/flynn/flynn-host/types"
)

func TestConfig(t *testing.T) {
	actual, err := parseConfig(bytes.NewBuffer([]byte(`{
		"resources": {
			"0": {
				"value": 10,
				"overcommit": true
			},
			"1": { "value": 20 }
		},
		"attributes": { "foo": "bar" },
		"rules": [
			{ "key": "0", "op": "==", "value": "10" },
			{ "key": "1", "op": "!=", "value": "20" },
			{ "key": "2", "op": ">", "value": "30" },
			{ "key": "3", "op": ">=", "value": "40" },
			{ "key": "4", "op": "<", "value": "50" },
			{ "key": "5", "op": "<=", "value": "60" }
		]
	}`)))
	if err != nil {
		t.Error(err)
	}

	expected := &host.Host{
		Resources:  map[string]host.ResourceValue{"0": {Value: 10, Overcommit: true}, "1": {Value: 20}},
		Attributes: map[string]string{"foo": "bar"},
		Rules: []host.Rule{
			{Key: "0", Op: host.OpEq, Value: "10"},
			{Key: "1", Op: host.OpNotEq, Value: "20"},
			{Key: "2", Op: host.OpGt, Value: "30"},
			{Key: "3", Op: host.OpGtEq, Value: "40"},
			{Key: "4", Op: host.OpLt, Value: "50"},
			{Key: "5", Op: host.OpLtEq, Value: "60"},
		},
	}

	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("incorrect config: got %#v, want %#v", actual, expected)
	}
}
