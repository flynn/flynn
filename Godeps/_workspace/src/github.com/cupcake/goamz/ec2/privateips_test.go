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

// Private IP tests with example responses

func (s *S) TestAssignPrivateIPAddressesExample(c *C) {
	testServer.Response(200, nil, AssignPrivateIpAddressesExample)

	resp, err := s.ec2.AssignPrivateIPAddresses("eni-id", []string{"1.2.3.4", "4.3.2.1"}, 0, true)
	req := testServer.WaitRequest()

	c.Assert(req.Form["Action"], DeepEquals, []string{"AssignPrivateIpAddresses"})
	c.Assert(req.Form["NetworkInterfaceId"], DeepEquals, []string{"eni-id"})
	c.Assert(req.Form["PrivateIpAddress.0"], DeepEquals, []string{"1.2.3.4"})
	c.Assert(req.Form["PrivateIpAddress.1"], DeepEquals, []string{"4.3.2.1"})
	c.Assert(req.Form["SecondaryPrivateIpAddressCount"], HasLen, 0)
	c.Assert(req.Form["AllowReassignment"], DeepEquals, []string{"true"})

	c.Assert(err, IsNil)
	c.Assert(resp.RequestId, Equals, "59dbff89-35bd-4eac-99ed-be587EXAMPLE")
}

func (s *S) TestUnassignPrivateIPAddressesExample(c *C) {
	testServer.Response(200, nil, UnassignPrivateIpAddressesExample)

	resp, err := s.ec2.UnassignPrivateIPAddresses("eni-id", []string{"1.2.3.4", "4.3.2.1"})
	req := testServer.WaitRequest()

	c.Assert(req.Form["Action"], DeepEquals, []string{"UnassignPrivateIpAddresses"})
	c.Assert(req.Form["NetworkInterfaceId"], DeepEquals, []string{"eni-id"})
	c.Assert(req.Form["PrivateIpAddress.0"], DeepEquals, []string{"1.2.3.4"})
	c.Assert(req.Form["PrivateIpAddress.1"], DeepEquals, []string{"4.3.2.1"})

	c.Assert(err, IsNil)
	c.Assert(resp.RequestId, Equals, "59dbff89-35bd-4eac-99ed-be587EXAMPLE")
}

// Private IP tests run live against EC2.

func (s *AmazonServerSuite) TestAssignUnassignPrivateIPs(c *C) {
	vpcResp, err := s.ec2.CreateVPC("10.7.0.0/16", "")
	c.Assert(err, IsNil)
	vpcId := vpcResp.VPC.Id
	defer s.deleteVPCs(c, []string{vpcId})

	subResp := s.createSubnet(c, vpcId, "10.7.1.0/24", "")
	subId := subResp.Subnet.Id
	defer s.deleteSubnets(c, []string{subId})

	// Launch a m1.small instance, so we can later assign up to 4
	// private IPs.
	instList, err := s.ec2.RunInstances(&ec2.RunInstances{
		ImageId:      imageId,
		InstanceType: "m1.small",
		SubnetId:     subId,
	})
	c.Assert(err, IsNil)
	inst := instList.Instances[0]
	c.Assert(inst, NotNil)
	instId := inst.InstanceId
	defer terminateInstances(c, s.ec2, []string{instId})

	// We need to wait for the instance to change state to 'running',
	// so its automatically created network interface on the created
	// subnet will appear.
	testAttempt := aws.AttemptStrategy{
		Total: 5 * time.Minute,
		Delay: 5 * time.Second,
	}
	var newNIC *ec2.NetworkInterface
	f := ec2.NewFilter()
	f.Add("subnet-id", subId)
	for a := testAttempt.Start(); a.Next(); {
		resp, err := s.ec2.NetworkInterfaces(nil, f)
		if err != nil {
			c.Logf("NetworkInterfaces returned: %v; retrying...", err)
			continue
		}
		for _, iface := range resp.Interfaces {
			c.Logf("found NIC %v", iface)
			if iface.Attachment.InstanceId == instId {
				c.Logf("found instance %v NIC", instId)
				newNIC = &iface
				break
			}
		}
		if newNIC != nil {
			break
		}
	}
	if newNIC == nil {
		c.Fatalf("timeout while waiting for the NIC to appear")
	}

	c.Check(newNIC.PrivateIPAddress, Matches, `^10\.7\.1\.\d+$`)
	c.Check(newNIC.PrivateIPs, HasLen, 1)

	// Now let's try assigning some more private IPs.
	ips := []string{"10.7.1.25", "10.7.1.30"}
	_, err = s.ec2.AssignPrivateIPAddresses(newNIC.Id, ips, 0, false)
	c.Assert(err, IsNil)

	expectIPs := append([]string{newNIC.PrivateIPAddress}, ips...)
	s.waitForAddresses(c, newNIC.Id, expectIPs)

	// And finally, unassign them.
	_, err = s.ec2.UnassignPrivateIPAddresses(newNIC.Id, ips)
	c.Assert(err, IsNil)

	expectIPs = []string{newNIC.PrivateIPAddress}
	s.waitForAddresses(c, newNIC.Id, expectIPs)
}

func (s *AmazonServerSuite) waitForAddresses(c *C, nicId string, ips []string) {
	// Wait for the given IPs to appear on the NIC, retrying if needed.
	testAttempt := aws.AttemptStrategy{
		Total: 5 * time.Minute,
		Delay: 5 * time.Second,
	}
	for a := testAttempt.Start(); a.Next(); {
		c.Logf("waiting for %v IPs on NIC %v", ips, nicId)
		resp, err := s.ec2.NetworkInterfaces([]string{nicId}, nil)
		if err != nil {
			c.Logf("NetworkInterfaces returned: %v; retrying...", err)
			continue
		}
		if len(resp.Interfaces) != 1 {
			c.Logf("found %d NICs; retrying", len(resp.Interfaces))
			continue
		}
		iface := resp.Interfaces[0]
		if len(iface.PrivateIPs) != len(ips) {
			c.Logf("addresses in %v: %v; still waiting", iface.Id, iface.PrivateIPs)
			continue
		}
		for i, ip := range iface.PrivateIPs {
			c.Assert(ip.Address, Equals, ips[i])
		}
		c.Logf("all addresses updated")
		return
	}
	c.Fatalf("timeout while waiting for the IPs to get updated")
}
