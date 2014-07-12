package main

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/flynn/flynn/host/types"
)

func TestConfig(t *testing.T) {
	actual, err := parseConfig(bytes.NewBuffer([]byte(`{ "metadata": { "foo": "bar" } }`)))
	if err != nil {
		t.Error(err)
	}

	expected := &host.Host{Metadata: map[string]string{"foo": "bar"}}

	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("incorrect config: got %#v, want %#v", actual, expected)
	}
}
