package dhcp4

import (
	"encoding/binary"
	"net"
	"time"
)

// SelectOrderOrAll has same functionality as SelectOrder, except if the order
// param is nil, whereby all options are added (in arbitrary order).
func (o Options) SelectOrderOrAll(order []byte) []Option {
	if order == nil {
		opts := make([]Option, 0, len(o))
		for i, v := range o {
			opts = append(opts, Option{Code: i, Value: v})
		}
		return opts
	}
	return o.SelectOrder(order)
}

// SelectOrder returns a slice of options ordered and selected by a byte array
// usually defined by OptionParameterRequestList.  This result is expected to be
// used in ReplyPacket()'s []Option parameter.
func (o Options) SelectOrder(order []byte) []Option {
	opts := make([]Option, 0, len(order))
	for _, v := range order {
		if data, ok := o[OptionCode(v)]; ok {
			opts = append(opts, Option{Code: OptionCode(v), Value: data})
		}
	}
	return opts
}

// IPRange returns how many ips in the ip range from start to stop (inclusive)
func IPRange(start, stop net.IP) int {
	return int(binary.BigEndian.Uint32(stop.To4())) - int(binary.BigEndian.Uint32(start.To4())) + 1
}

// IPAdd returns a copy of start + add.
// IPAdd(net.IP{192,168,1,1},30) returns net.IP{192.168.1.31}
func IPAdd(start net.IP, add int) net.IP { // IPv4 only
	start = start.To4()
	result := make(net.IP, 4)
	binary.BigEndian.PutUint32(result, binary.BigEndian.Uint32(start)+uint32(add))
	return result
}

// IPLess returns where IP a is less than IP b.
func IPLess(a, b net.IP) bool {
	b = b.To4()
	for i, ai := range a.To4() {
		if ai != b[i] {
			return ai < b[i]
		}
	}
	return false
}

// IPInRange returns true if ip is between (inclusive) start and stop.
func IPInRange(start, stop, ip net.IP) bool {
	return !(IPLess(ip, start) || IPLess(stop, ip))
}

// OptionsLeaseTime - converts a time.Duration to a 4 byte slice, compatible
// with OptionIPAddressLeaseTime.
func OptionsLeaseTime(d time.Duration) []byte {
	leaseBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(leaseBytes, uint32(d/time.Second))
	return leaseBytes
}

// JoinIPs returns a byte slice of IP addresses, one immediately after the other
// This may be useful for creating multiple IP options such as OptionRouter.
func JoinIPs(ips []net.IP) (b []byte) {
	for _, v := range ips {
		b = append(b, v.To4()...)
	}
	return
}
