//
// goamz - Go packages to interact with the Amazon Web Services.
//
//   https://wiki.ubuntu.com/goamz
//
// Copyright (c) 2014 Canonical Ltd.
//

package ec2

import (
	"strconv"
)

// AssignPrivateIPAddresses assigns secondary IP addresses to the
// network interface interfaceId.
//
// If secondaryIPCount is non-zero, ipAddresses must be nil, and the
// specified number of secondary IP addresses will be allocated within
// the subnet's CIDR block range.
//
// allowReassignment specifies whether to allow reassignment of
// addresses currently assigned to a different network interface.
//
// See http://goo.gl/MoeH0L more details.
func (ec2 *EC2) AssignPrivateIPAddresses(interfaceId string, ipAddresses []string, secondaryIPCount int, allowReassignment bool) (resp *SimpleResp, err error) {
	params := makeParamsVPC("AssignPrivateIpAddresses")
	params["NetworkInterfaceId"] = interfaceId
	if secondaryIPCount > 0 {
		params["SecondaryPrivateIpAddressCount"] = strconv.Itoa(secondaryIPCount)
	} else {
		for i, ip := range ipAddresses {
			// PrivateIpAddress is zero indexed.
			n := strconv.Itoa(i)
			params["PrivateIpAddress."+n] = ip
		}
	}
	if allowReassignment {
		params["AllowReassignment"] = "true"
	}
	resp = &SimpleResp{}
	err = ec2.query(params, resp)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// UnassignPrivateIPAddresses unassigns one or more secondary private
// IP addresses from a network interface.
//
// See http://goo.gl/RjGZdB for more details.
func (ec2 *EC2) UnassignPrivateIPAddresses(interfaceId string, ipAddresses []string) (resp *SimpleResp, err error) {
	params := makeParamsVPC("UnassignPrivateIpAddresses")
	params["NetworkInterfaceId"] = interfaceId
	for i, ip := range ipAddresses {
		// PrivateIpAddress is zero indexed.
		n := strconv.Itoa(i)
		params["PrivateIpAddress."+n] = ip
	}
	resp = &SimpleResp{}
	err = ec2.query(params, resp)
	if err != nil {
		return nil, err
	}
	return resp, nil
}
