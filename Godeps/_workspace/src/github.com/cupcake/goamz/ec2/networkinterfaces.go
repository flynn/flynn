//
// goamz - Go packages to interact with the Amazon Web Services.
//
//   https://wiki.ubuntu.com/goamz
//
// Copyright (c) 2014 Canonical Ltd.
//

package ec2

import (
	"fmt"
	"strconv"
)

// NetworkInterfaceAttachment describes a network interface
// attachment.
//
// See http://goo.gl/KtiKuV for more details.
type NetworkInterfaceAttachment struct {
	Id                  string `xml:"attachmentId"`
	InstanceId          string `xml:"instanceId"`
	InstanceOwnerId     string `xml:"instanceOwnerId"`
	DeviceIndex         int    `xml:"deviceIndex"`
	Status              string `xml:"status"`
	AttachTime          string `xml:"attachTime"`
	DeleteOnTermination bool   `xml:"deleteOnTermination"`
}

// PrivateIP describes a private IP address of a network interface.
//
// See http://goo.gl/jtuQEJ for more details.
type PrivateIP struct {
	Address   string `xml:"privateIpAddress"`
	DNSName   string `xml:"privateDnsName"`
	IsPrimary bool   `xml:"primary"`
}

// NetworkInterface describes a network interface for VPC.
//
// See http://goo.gl/G63OQL for more details.
type NetworkInterface struct {
	Id               string                     `xml:"networkInterfaceId"`
	SubnetId         string                     `xml:"subnetId"`
	VPCId            string                     `xml:"vpcId"`
	AvailZone        string                     `xml:"availabilityZone"`
	Description      string                     `xml:"description"`
	OwnerId          string                     `xml:"ownerId"`
	RequesterId      string                     `xml:"requesterId"`
	RequesterManaged bool                       `xml:"requesterManaged"`
	Status           string                     `xml:"status"`
	MACAddress       string                     `xml:"macAddress"`
	PrivateIPAddress string                     `xml:"privateIpAddress"`
	PrivateDNSName   string                     `xml:"privateDnsName"`
	SourceDestCheck  bool                       `xml:"sourceDestCheck"`
	Groups           []SecurityGroup            `xml:"groupSet>item"`
	Attachment       NetworkInterfaceAttachment `xml:"attachment"`
	Tags             []Tag                      `xml:"tagSet>item"`
	PrivateIPs       []PrivateIP                `xml:"privateIpAddressesSet>item"`
}

// CreateNetworkInterface encapsulates options for the
// CreateNetworkInterface call.
//
// SubnetId is the only required field.
//
// One or more private IP addresses can be specified by using the
// PrivateIPs slice. Only one provided PrivateIP may be set as
// primary.
//
// If PrivateIPs is empty, EC2 selects a primary private IP from the
// subnet range.
//
// When SecondaryPrivateIPCount is non-zero, EC2 allocates that
// number of IP addresses from within the subnet range.  The number of
// IP addresses you can assign to a network interface varies by
// instance type.
type CreateNetworkInterface struct {
	SubnetId                string
	PrivateIPs              []PrivateIP
	SecondaryPrivateIPCount int
	Description             string
	SecurityGroupIds        []string
}

// CreateNetworkInterfaceResp is the response to a
// CreateNetworkInterface request.
//
// See http://goo.gl/ze3VhA for more details.
type CreateNetworkInterfaceResp struct {
	RequestId        string           `xml:"requestId"`
	NetworkInterface NetworkInterface `xml:"networkInterface"`
}

// CreateNetworkInterface creates a network interface in the specified
// subnet.
//
// See http://goo.gl/ze3VhA for more details.
func (ec2 *EC2) CreateNetworkInterface(opts CreateNetworkInterface) (resp *CreateNetworkInterfaceResp, err error) {
	params := makeParamsVPC("CreateNetworkInterface")
	params["SubnetId"] = opts.SubnetId
	for i, ip := range opts.PrivateIPs {
		prefix := fmt.Sprintf("PrivateIpAddresses.%d.", i+1)
		params[prefix+"PrivateIpAddress"] = ip.Address
		params[prefix+"Primary"] = strconv.FormatBool(ip.IsPrimary)
	}
	if opts.Description != "" {
		params["Description"] = opts.Description
	}
	if opts.SecondaryPrivateIPCount > 0 {
		count := strconv.Itoa(opts.SecondaryPrivateIPCount)
		params["SecondaryPrivateIpAddressCount"] = count
	}
	for i, groupId := range opts.SecurityGroupIds {
		params["SecurityGroupId."+strconv.Itoa(i+1)] = groupId
	}
	resp = &CreateNetworkInterfaceResp{}
	err = ec2.query(params, resp)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// DeleteNetworkInterface deletes the specified network interface,
// which must have been previously detached.
//
// See http://goo.gl/MC1yOj for more details.
func (ec2 *EC2) DeleteNetworkInterface(id string) (resp *SimpleResp, err error) {
	params := makeParamsVPC("DeleteNetworkInterface")
	params["NetworkInterfaceId"] = id
	resp = &SimpleResp{}
	err = ec2.query(params, resp)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// NetworkInterfacesResp is the response to a NetworkInterfaces
// request.
//
// See http://goo.gl/2LcXtM for more details.
type NetworkInterfacesResp struct {
	RequestId  string             `xml:"requestId"`
	Interfaces []NetworkInterface `xml:"networkInterfaceSet>item"`
}

// NetworkInterfaces returns a list of network interfaces.
//
// If the ids or filter parameters are provided, only matching
// interfaces are returned.
//
// See http://goo.gl/2LcXtM for more details.
func (ec2 *EC2) NetworkInterfaces(ids []string, filter *Filter) (resp *NetworkInterfacesResp, err error) {
	params := makeParamsVPC("DescribeNetworkInterfaces")
	for i, id := range ids {
		params["NetworkInterfaceId."+strconv.Itoa(i+1)] = id
	}
	filter.addParams(params)

	resp = &NetworkInterfacesResp{}
	err = ec2.query(params, resp)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// AttachNetworkInterfaceResp is the response to an
// AttachNetworkInterface request.
//
// See http://goo.gl/rEbSii for more details.
type AttachNetworkInterfaceResp struct {
	RequestId    string `xml:"requestId"`
	AttachmentId string `xml:"attachmentId"`
}

// AttachNetworkInterface attaches a network interface to an instance.
//
// See http://goo.gl/rEbSii for more details.
func (ec2 *EC2) AttachNetworkInterface(interfaceId, instanceId string, deviceIndex int) (resp *AttachNetworkInterfaceResp, err error) {
	params := makeParamsVPC("AttachNetworkInterface")
	params["NetworkInterfaceId"] = interfaceId
	params["InstanceId"] = instanceId
	params["DeviceIndex"] = strconv.Itoa(deviceIndex)
	resp = &AttachNetworkInterfaceResp{}
	err = ec2.query(params, resp)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// DetachNetworkInterface detaches a network interface from an
// instance.
//
// See http://goo.gl/0Xc1px for more details.
func (ec2 *EC2) DetachNetworkInterface(attachmentId string, force bool) (resp *SimpleResp, err error) {
	params := makeParamsVPC("DetachNetworkInterface")
	params["AttachmentId"] = attachmentId
	if force {
		// Force is optional.
		params["Force"] = "true"
	}
	resp = &SimpleResp{}
	err = ec2.query(params, resp)
	if err != nil {
		return nil, err
	}
	return resp, nil
}
