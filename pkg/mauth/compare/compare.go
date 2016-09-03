// Package compare provides various compare operators with binary codecs
// useful for macaroon-based authorization.
package compare

import (
	"encoding/binary"
	"fmt"
	"net"
	"regexp"
)

type Type uint8

const (
	TypeFalse    Type = 0
	TypeTrue     Type = 1
	TypeIntegers Type = 2
	TypeStrings  Type = 3
	TypeRegexp   Type = 4
	TypeCIDRs    Type = 5
)

// comparisons are encoded as a single type byte followed by zero or more bytes
// that are specific to the comparison type.
//
// UINT8 - type
// UINT8 0..n - bytes

func UnmarshalBinary(b []byte) (interface{}, error) {
	if len(b) < 1 {
		return nil, fmt.Errorf("compare: buffer to unmarshal is zero-length")
	}
	switch Type(b[0]) {
	case TypeFalse, TypeTrue:
		v := new(Bool)
		return *v, v.UnmarshalBinary(b)
	case TypeIntegers:
		v := new(Integers)
		return *v, v.UnmarshalBinary(b)
	case TypeStrings:
		v := new(Strings)
		return *v, v.UnmarshalBinary(b)
	case TypeRegexp:
		v := new(Regexp)
		return v, v.UnmarshalBinary(b)
	case TypeCIDRs:
		v := new(CIDRs)
		return *v, v.UnmarshalBinary(b)
	default:
		return nil, fmt.Errorf("compare: unknown type %d", b[0])
	}
}

type Bool bool

func (b Bool) Compare(have bool) bool {
	return bool(b) == have
}

func (b Bool) MarshalBinary() ([]byte, error) {
	// type: 0x0 (false) OR 0x1 (true)

	if bool(b) {
		return []byte{byte(TypeTrue)}, nil
	} else {
		return []byte{byte(TypeFalse)}, nil
	}
}

func (b *Bool) UnmarshalBinary(buf []byte) error {
	if len(buf) < 1 {
		return fmt.Errorf("compare: missing type byte")
	}
	if buf[0] != byte(TypeTrue) && buf[0] != byte(TypeFalse) {
		return fmt.Errorf("compare: expected type %d or %d, got %d", TypeTrue, TypeFalse, buf[0])
	}
	*b = Bool(buf[0] == byte(TypeTrue))
	return nil
}

type IntegerOp uint8

const (
	IntegerOpEq  IntegerOp = 0
	IntegerOpGt  IntegerOp = 1
	IntegerOpLt  IntegerOp = 2
	IntegerOpGte IntegerOp = 3
	IntegerOpLte IntegerOp = 4

	integerOpMax = 4
)

type Integer struct {
	Op  IntegerOp
	Int int64
}

func (c Integer) Compare(have int64) bool {
	switch c.Op {
	case IntegerOpEq:
		return have == c.Int
	case IntegerOpGt:
		return have > c.Int
	case IntegerOpLt:
		return have < c.Int
	case IntegerOpGte:
		return have >= c.Int
	case IntegerOpLte:
		return have <= c.Int
	default:
		return false
	}
}

type Integers []Integer

func (c Integers) Compare(have int64) bool {
	for _, want := range c {
		if want.Compare(have) {
			return true
		}
	}
	return false
}

func (c Integers) MarshalBinary() ([]byte, error) {
	// type: 0x2
	// UINT8 - op
	// VARINT - int
	// ...

	buf := make([]byte, 1+(len(c)*(binary.MaxVarintLen64+1)))
	buf[0] = byte(TypeIntegers)
	i := 1
	for _, e := range c {
		buf[i] = byte(e.Op)
		i++
		i = i + binary.PutVarint(buf[i:], e.Int)
	}
	return buf[:i], nil
}

func (c *Integers) UnmarshalBinary(b []byte) error {
	if len(b) < 1 {
		return fmt.Errorf("compare: missing type byte")
	}
	if b[0] != byte(TypeIntegers) {
		return fmt.Errorf("compare: expected type %d, got %d", TypeIntegers, b[0])
	}
	b = b[1:]
	*c = Integers{}
	for len(b) > 0 {
		if len(b) < 2 {
			return fmt.Errorf("compare: unexpected end of buffer decoding integer")
		}
		op := IntegerOp(b[0])
		if op > integerOpMax {
			return fmt.Errorf("compare: unknown integer operation %d", b[0])
		}
		i, n := binary.Varint(b[1:])
		if n <= 0 {
			return fmt.Errorf("compare: invalid integer")
		}
		*c = append(*c, Integer{Op: op, Int: i})
		b = b[n+1:]
	}
	return nil
}

type Strings []string

func (ss Strings) Compare(have string) bool {
	for _, want := range ss {
		if want == have {
			return true
		}
	}
	return false
}

const maxStringLen = 65535

func (ss Strings) MarshalBinary() ([]byte, error) {
	// type: 0x3
	// UINT16 - string length N
	// N BYTES - string
	// ...

	l := 1
	for _, s := range ss {
		if len(s) > maxStringLen {
			return nil, fmt.Errorf("compare: %q is greater than the maximum length %d", s, maxStringLen)
		}
		l += len(s)
	}
	l += len(ss) * 2

	buf := make([]byte, 1, l)
	buf[0] = byte(TypeStrings)
	for _, s := range ss {
		buf = append(buf, byte(len(s)>>8), byte(len(s)))
		buf = append(buf, s...)
	}
	return buf, nil
}

func (ss *Strings) UnmarshalBinary(b []byte) error {
	if len(b) < 1 {
		return fmt.Errorf("compare: missing type byte")
	}
	if b[0] != byte(TypeStrings) {
		return fmt.Errorf("compare: expected type %d, got %d", TypeStrings, b[0])
	}
	b = b[1:]
	*ss = make([]string, 0)
	for len(b) > 0 {
		if len(b) < 2 {
			return fmt.Errorf("compare: remaining buffer length is less than a single uint16")
		}
		l := int(binary.BigEndian.Uint16(b))
		if len(b) < l+2 {
			return fmt.Errorf("compare: remaining buffer length is less than string length")
		}
		*ss = append(*ss, string(b[2:l+2]))
		b = b[l+2:]
	}
	return nil
}

type Regexp regexp.Regexp

func (r *Regexp) Compare(have string) bool {
	return (*regexp.Regexp)(r).MatchString(have)
}

func (r *Regexp) MarshalBinary() ([]byte, error) {
	// UINT8 - type: 0x4
	// BYTES - regexp

	s := (*regexp.Regexp)(r).String()
	buf := make([]byte, 1, len(s)+1)
	buf[0] = byte(TypeRegexp)
	buf = append(buf, s...)
	return buf, nil
}

func (r *Regexp) UnmarshalBinary(b []byte) error {
	if len(b) < 1 {
		return fmt.Errorf("compare: missing type byte")
	}
	if b[0] != byte(TypeRegexp) {
		return fmt.Errorf("compare: expected type %d, got %d", TypeRegexp, b[0])
	}

	exp, err := regexp.Compile(string(b[1:]))
	if err != nil {
		return fmt.Errorf("compare: error compiling regexp: %s", err)
	}
	*r = Regexp(*exp)
	return nil
}

type CIDRs []net.IPNet

func (c CIDRs) Compare(have net.IP) bool {
	for _, want := range c {
		if want.Contains(have) {
			return true
		}
	}
	return false
}

const (
	ipv6Len     = 16
	ipv6CIDRLen = ipv6Len + 1
	ipv4Len     = 4
	ipv4CIDRLen = ipv4Len + 1
)

func (c CIDRs) MarshalBinary() ([]byte, error) {
	// type: 0x5
	// UINT8 - count of one bits in the mask, top bit of the byte is 1 if IPv6, 0 if IPv4
	// N BYTES - 4 or 16 bytes of the IP address depending on v4 or v6 from the top bit of the mask byte
	// ...

	buf := make([]byte, 1, 1+(ipv6CIDRLen*len(c)))
	buf[0] = byte(TypeCIDRs)
	for _, n := range c {
		if v4 := n.IP.To4(); v4 != nil {
			ones, _ := n.Mask.Size()
			buf = append(buf, byte(ones))
			buf = append(buf, v4...)
		} else {
			ones, _ := n.Mask.Size()
			buf = append(buf, byte(ones)|(1<<7)) // use top bit to indicate IPv6
			buf = append(buf, n.IP...)
		}
	}
	return buf, nil
}

func (c *CIDRs) UnmarshalBinary(b []byte) error {
	if len(b) < 1 {
		return fmt.Errorf("compare: missing type byte")
	}
	if b[0] != byte(TypeCIDRs) {
		return fmt.Errorf("compare: expected type %d, got %d", TypeCIDRs, b[0])
	}
	b = b[1:]
	*c = make(CIDRs, 0, len(b)/ipv4CIDRLen)
	for len(b) > 0 {
		var n net.IPNet
		if b[0]&(1<<7) == 0 { // check top bit of ones byte, if 0, IPv4, if 1, IPv6
			if len(b) < ipv4CIDRLen {
				return fmt.Errorf("compare: unexpected end of buffer decoding IPv4 CIDR")
			}
			n.Mask = net.CIDRMask(int(b[0]), ipv4Len*8)
			n.IP = net.IPv4(b[1], b[2], b[3], b[4])
			b = b[5:]
		} else {
			if len(b) < ipv6CIDRLen {
				return fmt.Errorf("compare: unexpected end of buffer decoding IPv6 CIDR")
			}
			n.Mask = net.CIDRMask(int(b[0]&^(1<<7)), ipv6Len*8)
			n.IP = make([]byte, ipv6Len)
			copy(n.IP, b[1:])
			b = b[ipv6CIDRLen:]
		}
		*c = append(*c, n)
	}
	return nil
}
