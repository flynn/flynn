package compare

import (
	"encoding"
	"math"
	"net"
	"regexp"
	"testing"

	. "github.com/flynn/go-check"
)

// Hook gocheck up to the "go test" runner
func TestCompare(t *testing.T) { TestingT(t) }

type S struct{}

var _ = Suite(&S{})

func mustMarshal(v encoding.BinaryMarshaler) []byte {
	res, err := v.MarshalBinary()
	if err != nil {
		panic(err)
	}
	return res
}

func (S) TestUnmarshalRoundtrip(c *C) {
	for i, t := range []encoding.BinaryMarshaler{
		Bool(true),
		Bool(false),
		Integers{Integer{IntegerOpGt, 1}},
		Integers{},
		Strings{"foo", "bar"},
		Strings{},
		(*Regexp)(regexp.MustCompile(`\d`)),
		(*Regexp)(regexp.MustCompile("")),
		CIDRs{
			net.IPNet{IP: net.IPv4(127, 0, 0, 1), Mask: net.IPv4Mask(255, 255, 255, 0)},
			net.IPNet{IP: net.IPv6loopback, Mask: net.CIDRMask(8, 16*8)},
		},
		CIDRs{},
	} {
		c.Log(i)
		info := Commentf("i = %d", i)

		b, err := t.MarshalBinary()
		c.Assert(err, IsNil, info)

		parsed, err := UnmarshalBinary(b)
		c.Assert(err, IsNil, info)
		c.Assert(parsed, DeepEquals, t)
	}
}

func (S) TestUnmarshalInvalid(c *C) {
	_, err := UnmarshalBinary(nil)
	c.Assert(err, Not(IsNil))
	_, err = UnmarshalBinary([]byte{10})
	c.Assert(err, Not(IsNil))
}

func (S) TestBoolCompare(c *C) {
	c.Assert(Bool(true).Compare(true), Equals, true)
	c.Assert(Bool(true).Compare(false), Equals, false)
	c.Assert(Bool(false).Compare(false), Equals, true)
	c.Assert(Bool(false).Compare(true), Equals, false)
}

func (S) TestBoolUnmarshalInvalidType(c *C) {
	c.Assert(new(Bool).UnmarshalBinary([]byte{2}), Not(IsNil))
}

func (S) TestIntegersCompare(c *C) {
	for _, t := range []struct {
		s Integers
		t []int64
		f []int64
	}{
		{Integers{Integer{IntegerOpEq, 1}}, []int64{1}, []int64{-1, 0, 2, 10}},
		{Integers{Integer{IntegerOpGt, 1}}, []int64{2, 10}, []int64{-1, 0, 1}},
		{Integers{Integer{IntegerOpGte, 1}}, []int64{1, 2, 10}, []int64{-1, 0}},
		{Integers{Integer{IntegerOpLt, 1}}, []int64{0, -1, -10}, []int64{1, 2, 10}},
		{Integers{Integer{IntegerOpLte, 1}}, []int64{1, 0, -1, -10}, []int64{2, 10}},
		{Integers{Integer{IntegerOpEq, 1}, Integer{IntegerOpEq, 2}}, []int64{1, 2}, []int64{-1, 0, 3, 10}},
		{Integers{Integer{IntegerOpEq, math.MaxInt64}, Integer{IntegerOpEq, math.MaxInt32}}, []int64{math.MaxInt64, math.MaxInt32}, []int64{-1, 0, 3, 10}},
		{Integers{}, nil, []int64{1, 2, 3, 10}},
	} {
		for _, n := range t.t {
			c.Assert(t.s.Compare(n), Equals, true, Commentf("%#v", t))
		}
		for _, n := range t.f {
			c.Assert(t.s.Compare(n), Equals, false, Commentf("%#v", t))
		}
	}
}

func (S) TestIntegersUnmarshalInvalid(c *C) {
	for _, t := range []struct {
		d string
		b []byte
	}{
		{"missing integer", mustMarshal(Integers{Integer{IntegerOpEq, 1}})[:2]},
		{"missing bytes of integer", mustMarshal(Integers{Integer{IntegerOpEq, math.MaxInt64}})[:10]},
		{"invalid op", mustMarshal(Integers{Integer{10, 1}})},
	} {
		c.Assert(new(Integers).UnmarshalBinary(t.b), Not(IsNil), Commentf(t.d))
	}
}

func (S) TestStringsCompare(c *C) {
	for _, t := range []struct {
		s Strings
		t []string
		f []string
	}{
		{Strings{"foo"}, []string{"foo"}, []string{"bar"}},
		{Strings{"foo", "bar"}, []string{"foo", "bar"}, []string{"asdf"}},
		{Strings{}, nil, []string{"foo", "bar"}},
	} {
		for _, e := range t.t {
			c.Assert(t.s.Compare(e), Equals, true, Commentf("%#v", t))
		}
		for _, e := range t.f {
			c.Assert(t.s.Compare(e), Equals, false, Commentf("%#v", t))
		}
	}
}

func (S) TestStringsUnmarshalInvalid(c *C) {
	for _, t := range []struct {
		d string
		b []byte
	}{
		{"missing length byte, one string", mustMarshal(Strings{"a"})[:2]},
		{"missing length byte, two strings", mustMarshal(Strings{"a", "b"})[:5]},
		{"unexpected end", mustMarshal(Strings{"a", "b"})[:6]},
	} {
		c.Assert(new(Strings).UnmarshalBinary(t.b), Not(IsNil), Commentf(t.d))
	}
}

func (S) TestRegexpCompare(c *C) {
	r := (*Regexp)(regexp.MustCompile("a"))
	c.Assert(r.Compare("ab"), Equals, true)
	c.Assert(r.Compare("bb"), Equals, false)
}

func (S) TestRegexpUnmarshalInvalid(c *C) {
	c.Assert(new(Regexp).UnmarshalBinary(append([]byte{byte(TypeRegexp)}, "(("...)), Not(IsNil))
}

func (S) TestCIDRsCompare(c *C) {
	for _, t := range []struct {
		s CIDRs
		t []net.IP
		f []net.IP
	}{
		{CIDRs{
			net.IPNet{IP: net.IPv4(127, 0, 0, 1), Mask: net.IPv4Mask(255, 255, 255, 0)},
			net.IPNet{IP: net.IPv6loopback, Mask: net.CIDRMask(8, 16*8)},
		},
			[]net.IP{net.IPv4(127, 0, 0, 2), net.IPv4(127, 0, 0, 1), net.IPv6loopback},
			[]net.IP{net.IPv4(10, 10, 10, 1), net.IPv4(127, 1, 0, 1), net.IPv6interfacelocalallnodes},
		},
	} {
		for _, e := range t.t {
			c.Assert(t.s.Compare(e), Equals, true, Commentf("%#v", t))
		}
		for _, e := range t.f {
			c.Assert(t.s.Compare(e), Equals, false, Commentf("%#v", t))
		}
	}
}

func (S) TestCIDRsUnmarshalInvalid(c *C) {
	for _, t := range []struct {
		d string
		b []byte
	}{
		{"short ipv4", mustMarshal(CIDRs{net.IPNet{IP: net.IPv4(127, 0, 0, 1), Mask: net.IPv4Mask(255, 255, 255, 0)}})[:5]},
		{"short ipv6", mustMarshal(CIDRs{net.IPNet{IP: net.IPv6loopback, Mask: net.CIDRMask(8, 16*8)}})[:6]},
	} {
		c.Assert(new(CIDRs).UnmarshalBinary(t.b), Not(IsNil), Commentf(t.d))
	}
}
