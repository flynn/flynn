package resource

import (
	"reflect"
	"testing"

	"github.com/docker/go-units"
	"github.com/flynn/flynn/pkg/typeconv"
	. "github.com/flynn/go-check"
)

// Hook gocheck up to the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type S struct{}

var _ = Suite(S{})

func assertDefault(c *C, r Resources, types ...Type) {
	for _, typ := range types {
		actual, ok := r[typ]
		if !ok {
			c.Fatalf("%s resource not set", typ)
		}
		expected := defaults[typ]
		if !reflect.DeepEqual(actual, expected) {
			c.Fatalf("%s resource is not default, expected: %+v, actual: %+v", typ, expected, actual)
		}
	}
}

func (S) TestSetDefaultsNil(c *C) {
	var r Resources
	SetDefaults(&r)
	c.Assert(r, NotNil)
}

func (S) TestSetDefaultsEmpty(c *C) {
	r := make(Resources)
	SetDefaults(&r)
	assertDefault(c, r, TypeMemory, TypeMaxFD)
}

func (S) TestSetDefaultsRequest(c *C) {
	// not specifying Request should default it to the value of Limit
	r := Resources{TypeMemory: Spec{Limit: typeconv.Int64Ptr(512 * units.MiB)}}
	SetDefaults(&r)
	assertDefault(c, r, TypeMaxFD)
	mem, ok := r[TypeMemory]
	if !ok {
		c.Fatal("memory resource not set")
	}
	c.Assert(*mem.Request, Equals, *mem.Limit)
}
