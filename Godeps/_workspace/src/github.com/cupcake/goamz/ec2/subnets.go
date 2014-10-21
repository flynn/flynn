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

// Subnet describes an Amazon VPC subnet.
//
// See http://goo.gl/CdkvO2 for more details.
type Subnet struct {
	Id                  string `xml:"subnetId"`
	State               string `xml:"state"`
	VPCId               string `xml:"vpcId"`
	CIDRBlock           string `xml:"cidrBlock"`
	AvailableIPCount    int    `xml:"availableIpAddressCount"`
	AvailZone           string `xml:"availabilityZone"`
	DefaultForAZ        bool   `xml:"defaultForAz"`
	MapPublicIPOnLaunch bool   `xml:"mapPublicIpOnLaunch"`
	Tags                []Tag  `xml:"tagSet>item"`
}

// CreateSubnetResp is the response to a CreateSubnet request.
//
// See http://goo.gl/wLPhfI for more details.
type CreateSubnetResp struct {
	RequestId string `xml:"requestId"`
	Subnet    Subnet `xml:"subnet"`
}

// CreateSubnet creates a subnet in an existing VPC.
//
// The vpcId and cidrBlock parameters specify the VPC id and CIDR
// block respectively - these cannot be changed after creation. The
// subnet's CIDR block can be the same as the VPC's CIDR block
// (assuming a single subnet is wanted), or a subset of the VPC's CIDR
// block. If more than one subnet is created in a VPC, their CIDR
// blocks must not overlap. The smallest subnet (and VPC) that can be
// created uses a /28 netmask (16 IP addresses), and the largest uses
// a /16 netmask (65,536 IP addresses).
//
// availZone may be empty, an availability zone is automatically
// selected.
//
// See http://goo.gl/wLPhfI for more details.
func (ec2 *EC2) CreateSubnet(vpcId, cidrBlock, availZone string) (resp *CreateSubnetResp, err error) {
	params := makeParamsVPC("CreateSubnet")
	params["VpcId"] = vpcId
	params["CidrBlock"] = cidrBlock
	if availZone != "" {
		params["AvailabilityZone"] = availZone
	}
	resp = &CreateSubnetResp{}
	err = ec2.query(params, resp)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// DeleteSubnet deletes the specified subnet. All running instances in
// the subnet must have been terminated.
//
// See http://goo.gl/KmhcBM for more details.
func (ec2 *EC2) DeleteSubnet(id string) (resp *SimpleResp, err error) {
	params := makeParamsVPC("DeleteSubnet")
	params["SubnetId"] = id
	resp = &SimpleResp{}
	err = ec2.query(params, resp)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// SubnetsResp is the response to a Subnets request.
//
// See http://goo.gl/NTKQVI for more details.
type SubnetsResp struct {
	RequestId string   `xml:"requestId"`
	Subnets   []Subnet `xml:"subnetSet>item"`
}

// Subnets returns one or more subnets. Both parameters are optional,
// and if specified will limit the returned subnets to the matching
// ids or filtering rules.
//
// See http://goo.gl/NTKQVI for more details.
func (ec2 *EC2) Subnets(ids []string, filter *Filter) (resp *SubnetsResp, err error) {
	params := makeParamsVPC("DescribeSubnets")
	for i, id := range ids {
		params["SubnetId."+strconv.Itoa(i+1)] = id
	}
	filter.addParams(params)

	resp = &SubnetsResp{}
	err = ec2.query(params, resp)
	if err != nil {
		return nil, err
	}
	return resp, nil
}
