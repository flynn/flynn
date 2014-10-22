//
// goamz - Go packages to interact with the Amazon Web Services.
//
//   https://wiki.ubuntu.com/goamz
//
// Copyright (c) 2014 Canonical Ltd.
//

package ec2_test

import (
	"launchpad.net/goamz/aws"
	"launchpad.net/goamz/ec2"
	. "launchpad.net/gocheck"
	"time"
)

// Network interface tests with example responses

func (s *S) TestCreateNetworkInterfaceExample(c *C) {
	testServer.Response(200, nil, CreateNetworkInterfaceExample)

	resp, err := s.ec2.CreateNetworkInterface(ec2.CreateNetworkInterface{
		SubnetId: "subnet-b2a249da",
		PrivateIPs: []ec2.PrivateIP{
			{Address: "10.0.2.157", IsPrimary: true},
		},
		SecurityGroupIds: []string{"sg-1a2b3c4d"},
	})
	req := testServer.WaitRequest()

	c.Assert(req.Form["Action"], DeepEquals, []string{"CreateNetworkInterface"})
	c.Assert(req.Form["SubnetId"], DeepEquals, []string{"subnet-b2a249da"})
	c.Assert(req.Form["PrivateIpAddress"], HasLen, 0)
	c.Assert(
		req.Form["PrivateIpAddresses.1.PrivateIpAddress"],
		DeepEquals,
		[]string{"10.0.2.157"},
	)
	c.Assert(
		req.Form["PrivateIpAddresses.1.Primary"],
		DeepEquals,
		[]string{"true"},
	)
	c.Assert(req.Form["Description"], HasLen, 0)
	c.Assert(req.Form["SecurityGroupId.1"], DeepEquals, []string{"sg-1a2b3c4d"})

	c.Assert(err, IsNil)
	c.Assert(resp.RequestId, Equals, "8dbe591e-5a22-48cb-b948-dd0aadd55adf")
	iface := resp.NetworkInterface
	c.Check(iface.Id, Equals, "eni-cfca76a6")
	c.Check(iface.SubnetId, Equals, "subnet-b2a249da")
	c.Check(iface.VPCId, Equals, "vpc-c31dafaa")
	c.Check(iface.AvailZone, Equals, "ap-southeast-1b")
	c.Check(iface.Description, Equals, "")
	c.Check(iface.OwnerId, Equals, "251839141158")
	c.Check(iface.RequesterManaged, Equals, false)
	c.Check(iface.Status, Equals, "available")
	c.Check(iface.MACAddress, Equals, "02:74:b0:72:79:61")
	c.Check(iface.PrivateIPAddress, Equals, "10.0.2.157")
	c.Check(iface.SourceDestCheck, Equals, true)
	c.Check(iface.Groups, DeepEquals, []ec2.SecurityGroup{
		{Id: "sg-1a2b3c4d", Name: "default"},
	})
	c.Check(iface.Tags, HasLen, 0)
	c.Check(iface.PrivateIPs, DeepEquals, []ec2.PrivateIP{
		{Address: "10.0.2.157", IsPrimary: true},
	})
}

func (s *S) TestDeleteNetworkInterfaceExample(c *C) {
	testServer.Response(200, nil, DeleteNetworkInterfaceExample)

	resp, err := s.ec2.DeleteNetworkInterface("eni-id")
	req := testServer.WaitRequest()

	c.Assert(req.Form["Action"], DeepEquals, []string{"DeleteNetworkInterface"})
	c.Assert(req.Form["NetworkInterfaceId"], DeepEquals, []string{"eni-id"})

	c.Assert(err, IsNil)
	c.Assert(resp.RequestId, Equals, "e1c6d73b-edaa-4e62-9909-6611404e1739")
}

func (s *S) TestNetworkInterfacesExample(c *C) {
	testServer.Response(200, nil, DescribeNetworkInterfacesExample)

	ids := []string{"eni-0f62d866", "eni-a66ed5cf"}
	resp, err := s.ec2.NetworkInterfaces(ids, nil)
	req := testServer.WaitRequest()

	c.Assert(req.Form["Action"], DeepEquals, []string{"DescribeNetworkInterfaces"})
	c.Assert(req.Form["NetworkInterfaceId.1"], DeepEquals, []string{ids[0]})
	c.Assert(req.Form["NetworkInterfaceId.2"], DeepEquals, []string{ids[1]})

	c.Assert(err, IsNil)
	c.Assert(resp.RequestId, Equals, "fc45294c-006b-457b-bab9-012f5b3b0e40")
	c.Check(resp.Interfaces, HasLen, 2)
	iface := resp.Interfaces[0]
	c.Check(iface.Id, Equals, ids[0])
	c.Check(iface.SubnetId, Equals, "subnet-c53c87ac")
	c.Check(iface.VPCId, Equals, "vpc-cc3c87a5")
	c.Check(iface.AvailZone, Equals, "ap-southeast-1b")
	c.Check(iface.Description, Equals, "")
	c.Check(iface.OwnerId, Equals, "053230519467")
	c.Check(iface.RequesterManaged, Equals, false)
	c.Check(iface.Status, Equals, "in-use")
	c.Check(iface.MACAddress, Equals, "02:81:60:cb:27:37")
	c.Check(iface.PrivateIPAddress, Equals, "10.0.0.146")
	c.Check(iface.SourceDestCheck, Equals, true)
	c.Check(iface.Groups, DeepEquals, []ec2.SecurityGroup{
		{Id: "sg-3f4b5653", Name: "default"},
	})
	c.Check(iface.Attachment, DeepEquals, ec2.NetworkInterfaceAttachment{
		Id:                  "eni-attach-6537fc0c",
		InstanceId:          "i-22197876",
		InstanceOwnerId:     "053230519467",
		DeviceIndex:         0,
		Status:              "attached",
		AttachTime:          "2012-07-01T21:45:27.000Z",
		DeleteOnTermination: true,
	})
	c.Check(iface.PrivateIPs, DeepEquals, []ec2.PrivateIP{
		{Address: "10.0.0.146", IsPrimary: true},
		{Address: "10.0.0.148", IsPrimary: false},
		{Address: "10.0.0.150", IsPrimary: false},
	})
	c.Check(iface.Tags, HasLen, 0)

	iface = resp.Interfaces[1]
	c.Check(iface.Id, Equals, ids[1])
	c.Check(iface.SubnetId, Equals, "subnet-cd8a35a4")
	c.Check(iface.VPCId, Equals, "vpc-f28a359b")
	c.Check(iface.AvailZone, Equals, "ap-southeast-1b")
	c.Check(iface.Description, Equals, "Primary network interface")
	c.Check(iface.OwnerId, Equals, "053230519467")
	c.Check(iface.RequesterManaged, Equals, false)
	c.Check(iface.Status, Equals, "in-use")
	c.Check(iface.MACAddress, Equals, "02:78:d7:00:8a:1e")
	c.Check(iface.PrivateIPAddress, Equals, "10.0.1.233")
	c.Check(iface.SourceDestCheck, Equals, true)
	c.Check(iface.Groups, DeepEquals, []ec2.SecurityGroup{
		{Id: "sg-a2a0b2ce", Name: "quick-start-1"},
	})
	c.Check(iface.Attachment, DeepEquals, ec2.NetworkInterfaceAttachment{
		Id:                  "eni-attach-a99c57c0",
		InstanceId:          "i-886401dc",
		InstanceOwnerId:     "053230519467",
		DeviceIndex:         0,
		Status:              "attached",
		AttachTime:          "2012-06-27T20:08:44.000Z",
		DeleteOnTermination: true,
	})
	c.Check(iface.PrivateIPs, DeepEquals, []ec2.PrivateIP{
		{Address: "10.0.1.233", IsPrimary: true},
		{Address: "10.0.1.20", IsPrimary: false},
	})
	c.Check(iface.Tags, HasLen, 0)
}

func (s *S) TestAttachNetworkInterfaceExample(c *C) {
	testServer.Response(200, nil, AttachNetworkInterfaceExample)

	resp, err := s.ec2.AttachNetworkInterface("eni-id", "i-id", 0)
	req := testServer.WaitRequest()

	c.Assert(req.Form["Action"], DeepEquals, []string{"AttachNetworkInterface"})
	c.Assert(req.Form["NetworkInterfaceId"], DeepEquals, []string{"eni-id"})
	c.Assert(req.Form["InstanceId"], DeepEquals, []string{"i-id"})
	c.Assert(req.Form["DeviceIndex"], DeepEquals, []string{"0"})

	c.Assert(err, IsNil)
	c.Assert(resp.RequestId, Equals, "ace8cd1e-e685-4e44-90fb-92014d907212")
	c.Assert(resp.AttachmentId, Equals, "eni-attach-d94b09b0")
}

func (s *S) TestDetachNetworkInterfaceExample(c *C) {
	testServer.Response(200, nil, DetachNetworkInterfaceExample)

	resp, err := s.ec2.DetachNetworkInterface("eni-attach-id", true)
	req := testServer.WaitRequest()

	c.Assert(req.Form["Action"], DeepEquals, []string{"DetachNetworkInterface"})
	c.Assert(req.Form["AttachmentId"], DeepEquals, []string{"eni-attach-id"})
	c.Assert(req.Form["Force"], DeepEquals, []string{"true"})

	c.Assert(err, IsNil)
	c.Assert(resp.RequestId, Equals, "ce540707-0635-46bc-97da-33a8a362a0e8")
}

// Network interface tests run against either a local test server or
// live on EC2.

func (s *ServerTests) TestNetworkInterfaces(c *C) {
	vpcResp, err := s.ec2.CreateVPC("10.3.0.0/16", "")
	c.Assert(err, IsNil)
	vpcId := vpcResp.VPC.Id
	defer s.deleteVPCs(c, []string{vpcId})

	subResp := s.createSubnet(c, vpcId, "10.3.1.0/24", "")
	subId := subResp.Subnet.Id
	defer s.deleteSubnets(c, []string{subId})

	sg := s.makeTestGroupVPC(c, vpcId, "vpc-sg-1", "vpc test group1")
	defer s.deleteGroups(c, []ec2.SecurityGroup{sg})

	instList, err := s.ec2.RunInstances(&ec2.RunInstances{
		ImageId:      imageId,
		InstanceType: "t1.micro",
		SubnetId:     subId,
	})
	c.Assert(err, IsNil)
	inst := instList.Instances[0]
	c.Assert(inst, NotNil)
	instId := inst.InstanceId
	defer terminateInstances(c, s.ec2, []string{instId})

	ips1 := []ec2.PrivateIP{{Address: "10.3.1.10", IsPrimary: true}}
	resp1, err := s.ec2.CreateNetworkInterface(ec2.CreateNetworkInterface{
		SubnetId:    subId,
		PrivateIPs:  ips1,
		Description: "My first iface",
	})
	c.Assert(err, IsNil)
	assertNetworkInterface(c, resp1.NetworkInterface, "", subId, ips1)
	c.Check(resp1.NetworkInterface.Description, Equals, "My first iface")
	id1 := resp1.NetworkInterface.Id

	ips2 := []ec2.PrivateIP{
		{Address: "10.3.1.20", IsPrimary: true},
		{Address: "10.3.1.22", IsPrimary: false},
	}
	resp2, err := s.ec2.CreateNetworkInterface(ec2.CreateNetworkInterface{
		SubnetId:         subId,
		PrivateIPs:       ips2,
		SecurityGroupIds: []string{sg.Id},
	})
	c.Assert(err, IsNil)
	assertNetworkInterface(c, resp2.NetworkInterface, "", subId, ips2)
	c.Assert(resp2.NetworkInterface.Groups, DeepEquals, []ec2.SecurityGroup{sg})
	id2 := resp2.NetworkInterface.Id

	// We only check for the network interfaces we just created,
	// because the user might have others in his account (when testing
	// against the EC2 servers). In some cases it takes a short while
	// until both interfaces are created, so we need to retry a few
	// times to make sure.
	testAttempt := aws.AttemptStrategy{
		Total: 5 * time.Minute,
		Delay: 5 * time.Second,
	}
	var list *ec2.NetworkInterfacesResp
	done := false
	for a := testAttempt.Start(); a.Next(); {
		c.Logf("waiting for %v to be created", []string{id1, id2})
		list, err = s.ec2.NetworkInterfaces(nil, nil)
		if err != nil {
			c.Logf("retrying; NetworkInterfaces returned: %v", err)
			continue
		}
		found := 0
		for _, iface := range list.Interfaces {
			c.Logf("found NIC %v", iface)
			switch iface.Id {
			case id1:
				assertNetworkInterface(c, iface, id1, subId, ips1)
				found++
			case id2:
				assertNetworkInterface(c, iface, id2, subId, ips2)
				found++
			}
			if found == 2 {
				done = true
				break
			}
		}
		if done {
			c.Logf("all NICs were created")
			break
		}
	}
	if !done {
		c.Fatalf("timeout while waiting for NICs %v", []string{id1, id2})
	}

	list, err = s.ec2.NetworkInterfaces([]string{id1}, nil)
	c.Assert(err, IsNil)
	c.Assert(list.Interfaces, HasLen, 1)
	assertNetworkInterface(c, list.Interfaces[0], id1, subId, ips1)

	f := ec2.NewFilter()
	f.Add("network-interface-id", id2)
	list, err = s.ec2.NetworkInterfaces(nil, f)
	c.Assert(err, IsNil)
	c.Assert(list.Interfaces, HasLen, 1)
	assertNetworkInterface(c, list.Interfaces[0], id2, subId, ips2)

	// Attachment might fail if the instance is not running yet,
	// so we retry for a while until it succeeds.
	var attResp *ec2.AttachNetworkInterfaceResp
	for a := testAttempt.Start(); a.Next(); {
		attResp, err = s.ec2.AttachNetworkInterface(id2, instId, 1)
		if err != nil {
			c.Logf("AttachNetworkInterface returned: %v; retrying...", err)
			attResp = nil
			continue
		}
		c.Logf("AttachNetworkInterface succeeded")
		c.Check(attResp.AttachmentId, Not(Equals), "")
		break
	}
	if attResp == nil {
		c.Fatalf("timeout while waiting for AttachNetworkInterface to succeed")
	}

	list, err = s.ec2.NetworkInterfaces([]string{id2}, nil)
	c.Assert(err, IsNil)
	att := list.Interfaces[0].Attachment
	c.Check(att.Id, Equals, attResp.AttachmentId)
	c.Check(att.InstanceId, Equals, instId)
	c.Check(att.DeviceIndex, Equals, 1)
	c.Check(att.Status, Matches, "(attaching|in-use)")

	_, err = s.ec2.DetachNetworkInterface(att.Id, true)
	c.Check(err, IsNil)

	_, err = s.ec2.DeleteNetworkInterface(id1)
	c.Assert(err, IsNil)

	// We might not be able to delete the interface until the
	// detachment is completed, so we need to retry here as well.
	for a := testAttempt.Start(); a.Next(); {
		_, err = s.ec2.DeleteNetworkInterface(id2)
		if err != nil {
			c.Logf("DeleteNetworkInterface returned: %v; retrying...", err)
			continue
		}
		c.Logf("DeleteNetworkInterface succeeded")
		return
	}
	c.Fatalf("timeout while waiting for DeleteNetworkInterface to succeed")
}

func assertNetworkInterface(c *C, obtained ec2.NetworkInterface, expectId, expectSubId string, expectIPs []ec2.PrivateIP) {
	if expectId != "" {
		c.Check(obtained.Id, Equals, expectId)
	} else {
		c.Check(obtained.Id, Matches, `^eni-[0-9a-f]+$`)
	}
	c.Check(obtained.Status, Matches, "(available|pending|in-use)")
	if expectSubId != "" {
		c.Check(obtained.SubnetId, Equals, expectSubId)
	} else {
		c.Check(obtained.SubnetId, Matches, `^subnet-[0-9a-f]+$`)
	}
	c.Check(obtained.Attachment, DeepEquals, ec2.NetworkInterfaceAttachment{})
	if len(expectIPs) > 0 {
		c.Check(obtained.PrivateIPs, DeepEquals, expectIPs)
		c.Check(obtained.PrivateIPAddress, DeepEquals, expectIPs[0].Address)
	}
}
