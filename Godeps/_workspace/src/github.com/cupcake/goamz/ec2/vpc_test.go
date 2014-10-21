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

// VPC tests with example responses

func (s *S) TestCreateVPCExample(c *C) {
	testServer.Response(200, nil, CreateVpcExample)

	resp, err := s.ec2.CreateVPC("10.0.0.0/16", "default")
	req := testServer.WaitRequest()

	c.Assert(req.Form["Action"], DeepEquals, []string{"CreateVpc"})
	c.Assert(req.Form["CidrBlock"], DeepEquals, []string{"10.0.0.0/16"})
	c.Assert(req.Form["InstanceTenancy"], DeepEquals, []string{"default"})

	c.Assert(err, IsNil)
	c.Assert(resp.RequestId, Equals, "7a62c49f-347e-4fc4-9331-6e8eEXAMPLE")
	vpc := resp.VPC
	c.Check(vpc.Id, Equals, "vpc-1a2b3c4d")
	c.Check(vpc.State, Equals, "pending")
	c.Check(vpc.CIDRBlock, Equals, "10.0.0.0/16")
	c.Check(vpc.DHCPOptionsId, Equals, "dopt-1a2b3c4d2")
	c.Check(vpc.Tags, HasLen, 0)
	c.Check(vpc.IsDefault, Equals, false)
	c.Check(vpc.InstanceTenancy, Equals, "default")
}

func (s *S) TestDeleteVPCExample(c *C) {
	testServer.Response(200, nil, DeleteVpcExample)

	resp, err := s.ec2.DeleteVPC("vpc-id")
	req := testServer.WaitRequest()

	c.Assert(req.Form["Action"], DeepEquals, []string{"DeleteVpc"})
	c.Assert(req.Form["VpcId"], DeepEquals, []string{"vpc-id"})

	c.Assert(err, IsNil)
	c.Assert(resp.RequestId, Equals, "7a62c49f-347e-4fc4-9331-6e8eEXAMPLE")
}

func (s *S) TestVPCsExample(c *C) {
	testServer.Response(200, nil, DescribeVpcsExample)

	resp, err := s.ec2.VPCs([]string{"vpc-1a2b3c4d"}, nil)
	req := testServer.WaitRequest()

	c.Assert(req.Form["Action"], DeepEquals, []string{"DescribeVpcs"})
	c.Assert(req.Form["VpcId.1"], DeepEquals, []string{"vpc-1a2b3c4d"})

	c.Assert(err, IsNil)
	c.Assert(resp.RequestId, Equals, "7a62c49f-347e-4fc4-9331-6e8eEXAMPLE")
	c.Assert(resp.VPCs, HasLen, 1)
	vpc := resp.VPCs[0]
	c.Check(vpc.Id, Equals, "vpc-1a2b3c4d")
	c.Check(vpc.State, Equals, "available")
	c.Check(vpc.CIDRBlock, Equals, "10.0.0.0/23")
	c.Check(vpc.DHCPOptionsId, Equals, "dopt-7a8b9c2d")
	c.Check(vpc.Tags, HasLen, 0)
	c.Check(vpc.IsDefault, Equals, false)
	c.Check(vpc.InstanceTenancy, Equals, "default")
}

// VPC tests to run against either a local test server or live on EC2.

func (s *ServerTests) TestVPCs(c *C) {
	resp1, err := s.ec2.CreateVPC("10.0.0.0/16", "")
	c.Assert(err, IsNil)
	assertVPC(c, resp1.VPC, "", "10.0.0.0/16")
	id1 := resp1.VPC.Id

	resp2, err := s.ec2.CreateVPC("10.1.0.0/16", "default")
	c.Assert(err, IsNil)
	assertVPC(c, resp2.VPC, "", "10.1.0.0/16")
	id2 := resp2.VPC.Id

	// We only check for the VPCs we just created, because the user
	// might have others in his account (when testing against the EC2
	// servers). In some cases it takes a short while until both VPCs
	// are created, so we need to retry a few times to make sure.
	var list *ec2.VPCsResp
	done := false
	testAttempt := aws.AttemptStrategy{
		Total: 2 * time.Minute,
		Delay: 5 * time.Second,
	}
	for a := testAttempt.Start(); a.Next(); {
		c.Logf("waiting for %v to be created", []string{id1, id2})
		list, err = s.ec2.VPCs(nil, nil)
		if err != nil {
			c.Logf("retrying; VPCs returned: %v", err)
			continue
		}
		found := 0
		for _, vpc := range list.VPCs {
			c.Logf("found VPC %v", vpc)
			switch vpc.Id {
			case id1:
				assertVPC(c, vpc, id1, resp1.VPC.CIDRBlock)
				found++
			case id2:
				assertVPC(c, vpc, id2, resp2.VPC.CIDRBlock)
				found++
			}
			if found == 2 {
				done = true
				break
			}
		}
		if done {
			c.Logf("all VPCs were created")
			break
		}
	}
	if !done {
		c.Fatalf("timeout while waiting for VPCs %v", []string{id1, id2})
	}

	list, err = s.ec2.VPCs([]string{id1}, nil)
	c.Assert(err, IsNil)
	c.Assert(list.VPCs, HasLen, 1)
	assertVPC(c, list.VPCs[0], id1, resp1.VPC.CIDRBlock)

	f := ec2.NewFilter()
	f.Add("cidr", resp2.VPC.CIDRBlock)
	list, err = s.ec2.VPCs(nil, f)
	c.Assert(err, IsNil)
	c.Assert(list.VPCs, HasLen, 1)
	assertVPC(c, list.VPCs[0], id2, resp2.VPC.CIDRBlock)

	_, err = s.ec2.DeleteVPC(id1)
	c.Assert(err, IsNil)
	_, err = s.ec2.DeleteVPC(id2)
	c.Assert(err, IsNil)
}

// deleteVPCs ensures the given VPCs are deleted, by retrying until a
// timeout or all VPC cannot be found anymore.  This should be used to
// make sure tests leave no VPCs around.
func (s *ServerTests) deleteVPCs(c *C, ids []string) {
	testAttempt := aws.AttemptStrategy{
		Total: 2 * time.Minute,
		Delay: 5 * time.Second,
	}
	for a := testAttempt.Start(); a.Next(); {
		deleted := 0
		c.Logf("deleting VPCs %v", ids)
		for _, id := range ids {
			_, err := s.ec2.DeleteVPC(id)
			if err == nil || errorCode(err) == "InvalidVpcID.NotFound" {
				c.Logf("VPC %s deleted", id)
				deleted++
				continue
			}
			if err != nil {
				c.Logf("retrying; DeleteVPC returned: %v", err)
			}
		}
		if deleted == len(ids) {
			c.Logf("all VPCs deleted")
			return
		}
	}
	c.Fatalf("timeout while waiting %v VPCs to get deleted!", ids)
}

func assertVPC(c *C, obtained ec2.VPC, expectId, expectCidr string) {
	if expectId != "" {
		c.Check(obtained.Id, Equals, expectId)
	} else {
		c.Check(obtained.Id, Matches, `^vpc-[0-9a-f]+$`)
	}
	c.Check(obtained.State, Matches, "(available|pending)")
	if expectCidr != "" {
		c.Check(obtained.CIDRBlock, Equals, expectCidr)
	} else {
		c.Check(obtained.CIDRBlock, Matches, `^\d+\.\d+\.\d+\.\d+/\d+$`)
	}
	c.Check(obtained.DHCPOptionsId, Matches, `^dopt-[0-9a-f]+$`)
	c.Check(obtained.IsDefault, Equals, false)
	c.Check(obtained.Tags, HasLen, 0)
	c.Check(obtained.InstanceTenancy, Matches, "(default|dedicated)")
}
