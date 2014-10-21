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

// VPC describes a Virtual Private Cloud (VPC).
//
// See http://goo.gl/Uy6ZLL for more details.
type VPC struct {
	Id              string `xml:"vpcId"`
	State           string `xml:"state"`
	CIDRBlock       string `xml:"cidrBlock"`
	DHCPOptionsId   string `xml:"dhcpOptionsId"`
	Tags            []Tag  `xml:"tagSet>item"`
	InstanceTenancy string `xml:"instanceTenancy"`
	IsDefault       bool   `xml:"isDefault"`
}

// CreateVPCResp is the response to a CreateVPC request.
//
// See http://goo.gl/nkwjvN for more details.
type CreateVPCResp struct {
	RequestId string `xml:"requestId"`
	VPC       VPC    `xml:"vpc"`
}

// CreateVPC creates a VPC with the specified CIDR block.
//
// The smallest VPC that can be created uses a /28 netmask (16 IP
// addresses), and the largest uses a /16 netmask (65,536 IP
// addresses).
//
// The instanceTenancy value holds the tenancy options for instances
// launched into the VPC - either "default" or "dedicated".
//
// See http://goo.gl/nkwjvN for more details.
func (ec2 *EC2) CreateVPC(CIDRBlock, instanceTenancy string) (resp *CreateVPCResp, err error) {
	params := makeParamsVPC("CreateVpc")
	params["CidrBlock"] = CIDRBlock
	if instanceTenancy == "" {
		instanceTenancy = "default"
	}
	params["InstanceTenancy"] = instanceTenancy
	resp = &CreateVPCResp{}
	err = ec2.query(params, resp)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// DeleteVPC deletes the VPC with the specified id. All gateways and
// resources that are associated with the VPC must have been
// previously deleted, including instances running in the VPC, and
// non-default security groups and route tables associated with the
// VPC.
//
// See http://goo.gl/bcxtbf for more details.
func (ec2 *EC2) DeleteVPC(id string) (resp *SimpleResp, err error) {
	params := makeParamsVPC("DeleteVpc")
	params["VpcId"] = id
	resp = &SimpleResp{}
	err = ec2.query(params, resp)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// VPCsResp is the response to a VPCs request.
//
// See http://goo.gl/Y5kHqG for more details.
type VPCsResp struct {
	RequestId string `xml:"requestId"`
	VPCs      []VPC  `xml:"vpcSet>item"`
}

// VPCs describes one or more VPCs. Both parameters are optional, and
// if specified will limit the returned VPCs to the matching ids or
// filtering rules.
//
// See http://goo.gl/Y5kHqG for more details.
func (ec2 *EC2) VPCs(ids []string, filter *Filter) (resp *VPCsResp, err error) {
	params := makeParamsVPC("DescribeVpcs")
	for i, id := range ids {
		params["VpcId."+strconv.Itoa(i+1)] = id
	}
	filter.addParams(params)

	resp = &VPCsResp{}
	err = ec2.query(params, resp)
	if err != nil {
		return nil, err
	}
	return resp, nil
}
