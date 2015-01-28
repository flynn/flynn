package data

import (
	"encoding/json"
	"testing"

	. "gopkg.in/check.v1"
)

// Hook up gocheck into the "go test" runner.
func Test(t *testing.T) { TestingT(t) }

type HexBytesSuite struct{}

var _ = Suite(&HexBytesSuite{})

func (HexBytesSuite) TestUnmarshalJSON(c *C) {
	var data HexBytes
	err := json.Unmarshal([]byte(`"666f6f"`), &data)
	c.Assert(err, IsNil)
	c.Assert(string(data), Equals, "foo")
}

func (HexBytesSuite) TestUnmarshalJSONError(c *C) {
	var data HexBytes

	// uneven length
	err := json.Unmarshal([]byte(`"a"`), &data)
	c.Assert(err, Not(IsNil))

	// invalid hex
	err = json.Unmarshal([]byte(`"zz"`), &data)
	c.Assert(err, Not(IsNil))

	// wrong type
	err = json.Unmarshal([]byte("6"), &data)
	c.Assert(err, Not(IsNil))
}

func (HexBytesSuite) TestMarshalJSON(c *C) {
	data, err := json.Marshal(HexBytes("foo"))
	c.Assert(err, IsNil)
	c.Assert(data, DeepEquals, []byte(`"666f6f"`))
}
