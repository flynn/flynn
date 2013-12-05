package main

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/flynn/sampi/types"
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

	expected := &sampi.Host{
		Resources:  map[string]sampi.ResourceValue{"0": {Value: 10, Overcommit: true}, "1": {Value: 20}},
		Attributes: map[string]string{"foo": "bar"},
		Rules: []sampi.Rule{
			{Key: "0", Op: sampi.OpEq, Value: "10"},
			{Key: "1", Op: sampi.OpNotEq, Value: "20"},
			{Key: "2", Op: sampi.OpGt, Value: "30"},
			{Key: "3", Op: sampi.OpGtEq, Value: "40"},
			{Key: "4", Op: sampi.OpLt, Value: "50"},
			{Key: "5", Op: sampi.OpLtEq, Value: "60"},
		},
	}

	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("incorrect config: got %#v, want %#v", actual, expected)
	}
}
