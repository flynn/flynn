package bootstrap

import (
	"testing"
)

type TestAction struct {
	Foo string
	Bar int
}

func (t *TestAction) Run(state *State) error {
	return nil
}

func init() {
	Register("test-action", &TestAction{})
}

func TestRun(t *testing.T) {
	manifest := []byte(`[{"id":"1", "action":"test-action", "Foo":"bar"}, {"id":"2", "action":"test-action", "Bar":2}]`)
	err := Run(manifest)
	if err != nil {
		t.Fatal(err)
	}
}
