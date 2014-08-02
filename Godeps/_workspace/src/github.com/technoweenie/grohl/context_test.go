package grohl

import (
	"testing"
)

func TestContext(t *testing.T) {
	empty := NewContext(nil)

	empty.Add("a", 1)
	empty.Add("b", 1)

	merged := empty.Merge(Data{"b": 2, "c": 3})

	expected := "a=1 b=2 c=3"
	if log := BuildLog(merged, false); log != expected {
		t.Errorf("Expected: %s\nActual: %s", expected, log)
	}
}
