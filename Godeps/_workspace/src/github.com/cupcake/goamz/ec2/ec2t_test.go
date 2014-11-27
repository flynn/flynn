package ec2_test

import (
	"fmt"
	"net"
	"regexp"
	"sort"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/cupcake/goamz/aws"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/cupcake/goamz/ec2"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/cupcake/goamz/ec2/ec2test"
	"github.com/cupcake/goamz/testutil"
	. "launchpad.net/gocheck"
)

// LocalServer represents a local ec2test fake server.
type LocalServer struct {
	auth   aws.Auth
	region aws.Region
	srv    *ec2test.Server
}

func (s *LocalServer) SetUp(c *C) {
	srv, err := ec2test.NewServer()
	c.Assert(err, IsNil)
	c.Assert(srv, NotNil)

	s.srv = srv
	s.region = aws.Region{EC2Endpoint: srv.URL()}
}

// LocalServerSuite defines tests that will run
// against the local ec2test server. It includes
// selected tests from ClientTests;
// when the ec2test functionality is sufficient, it should
// include all of them, and ClientTests can be simply embedded.
type LocalServerSuite struct {
	srv LocalServer
	ServerTests
	clientTests ClientTests
}

var _ = Suite(&LocalServerSuite{})

func (s *LocalServerSuite) SetUpSuite(c *C) {
	s.srv.SetUp(c)
	s.ServerTests.ec2 = ec2.New(s.srv.auth, s.srv.region)
	s.clientTests.ec2 = ec2.New(s.srv.auth, s.srv.region)
}

func (s *LocalServerSuite) TestRunAndTerminate(c *C) {
	s.clientTests.TestRunAndTerminate(c)
}

func (s *LocalServerSuite) TestSecurityGroups(c *C) {
	s.clientTests.TestSecurityGroups(c)
}

// TestUserData is not defined on ServerTests because it
// requires the ec2test server to function.
func (s *LocalServerSuite) TestUserData(c *C) {
	data := make([]byte, 256)
	for i := range data {
		data[i] = byte(i)
	}
	inst, err := s.ec2.RunInstances(&ec2.RunInstances{
		ImageId:      imageId,
		InstanceType: "t1.micro",
		UserData:     data,
	})
	c.Assert(err, IsNil)
	c.Assert(inst, NotNil)

	id := inst.Instances[0].InstanceId

	defer s.ec2.TerminateInstances([]string{id})

	tinst := s.srv.srv.Instance(id)
	c.Assert(tinst, NotNil)
	c.Assert(tinst.UserData, DeepEquals, data)
}

func (s *LocalServerSuite) TestInstanceInfo(c *C) {
	list, err := s.ec2.RunInstances(&ec2.RunInstances{
		ImageId:      imageId,
		InstanceType: "t1.micro",
	})
	c.Assert(err, IsNil)

	inst := list.Instances[0]
	c.Assert(inst, NotNil)

	id := inst.InstanceId
	defer s.ec2.TerminateInstances([]string{id})

	masked := func(addr string) string {
		return net.ParseIP(addr).Mask(net.CIDRMask(24, 32)).String()
	}
	c.Check(masked(inst.IPAddress), Equals, "8.0.0.0")
	c.Check(masked(inst.PrivateIPAddress), Equals, "127.0.0.0")
	// DNSName is empty initially, to check it we need to refresh.
	c.Check(inst.DNSName, Equals, "")
	c.Check(inst.PrivateDNSName, Equals, id+".internal.invalid")

	// Get the instance again to verify DNSName.
	resp, err := s.ec2.Instances([]string{id}, nil)
	c.Assert(err, IsNil)
	c.Assert(resp.Reservations, HasLen, 1)
	c.Assert(resp.Reservations[0].Instances, HasLen, 1)
	c.Check(resp.Reservations[0].Instances[0].DNSName, Equals, id+".testing.invalid")
}

// AmazonServerSuite runs the ec2test server tests against a live EC2 server.
// It will only be activated if the -amazon flag is specified.
type AmazonServerSuite struct {
	srv AmazonServer
	ServerTests
}

var _ = Suite(&AmazonServerSuite{})

func (s *AmazonServerSuite) SetUpSuite(c *C) {
	if !testutil.Amazon {
		c.Skip("AmazonServerSuite tests not enabled")
	}
	s.srv.SetUp(c)
	s.ServerTests.ec2 = ec2.New(s.srv.auth, aws.USEast)
}

// ServerTests defines a set of tests designed to test
// the ec2test local fake ec2 server.
// It is not used as a test suite in itself, but embedded within
// another type.
type ServerTests struct {
	ec2 *ec2.EC2
}

func terminateInstances(c *C, e *ec2.EC2, ids []string) {
	_, err := e.TerminateInstances(ids)
	c.Assert(err, IsNil, Commentf("%v INSTANCES LEFT RUNNING!!!", ids))
	// We need to wait until the instances are really off, because
	// entities that depend on them won't be deleted (i.e. groups,
	// NICs, subnets, etc.)
	testAttempt := aws.AttemptStrategy{
		Total: 10 * time.Minute,
		Delay: 5 * time.Second,
	}
	f := ec2.NewFilter()
	f.Add("instance-state-name", "terminated")
	idsLeft := make(map[string]bool)
	for _, id := range ids {
		idsLeft[id] = true
	}
	for a := testAttempt.Start(); a.Next(); {
		c.Logf("waiting for %v to get terminated", ids)
		resp, err := e.Instances(ids, f)
		if err != nil {
			c.Fatalf("not waiting for %v to terminate: %v", ids, err)
		}
		for _, r := range resp.Reservations {
			for _, inst := range r.Instances {
				delete(idsLeft, inst.InstanceId)
			}
		}
		ids = []string{}
		for id, _ := range idsLeft {
			ids = append(ids, id)
		}
		if len(ids) == 0 {
			c.Logf("all instances terminated.")
			return
		}
	}
	c.Fatalf("%v INSTANCES LEFT RUNNING!!!", ids)
}

func (s *ServerTests) makeTestGroup(c *C, name, descr string) ec2.SecurityGroup {
	return s.makeTestGroupVPC(c, "", name, descr)
}

func (s *ServerTests) makeTestGroupVPC(c *C, vpcId, name, descr string) ec2.SecurityGroup {
	// Clean it up if a previous test left it around.
	_, err := s.ec2.DeleteSecurityGroup(ec2.SecurityGroup{Name: name})
	if err != nil && errorCode(err) != "InvalidGroup.NotFound" {
		c.Fatalf("delete security group: %v", err)
	}

	resp, err := s.ec2.CreateSecurityGroupVPC(vpcId, name, descr)
	c.Assert(err, IsNil)
	c.Assert(resp.Name, Equals, name)
	return resp.SecurityGroup
}

func (s *ServerTests) TestIPPerms(c *C) {
	g0 := s.makeTestGroup(c, "goamz-test0", "ec2test group 0")
	g1 := s.makeTestGroup(c, "goamz-test1", "ec2test group 1")
	defer s.deleteGroups(c, []ec2.SecurityGroup{g0, g1})

	resp, err := s.ec2.SecurityGroups([]ec2.SecurityGroup{g0, g1}, nil)
	c.Assert(err, IsNil)
	c.Assert(resp.Groups, HasLen, 2)
	c.Assert(resp.Groups[0].IPPerms, HasLen, 0)
	c.Assert(resp.Groups[1].IPPerms, HasLen, 0)

	ownerId := resp.Groups[0].OwnerId

	// test some invalid parameters
	// TODO more
	_, err = s.ec2.AuthorizeSecurityGroup(g0, []ec2.IPPerm{{
		Protocol:  "tcp",
		FromPort:  0,
		ToPort:    1024,
		SourceIPs: []string{"z127.0.0.1/24"},
	}})
	c.Assert(err, NotNil)
	c.Check(errorCode(err), Equals, "InvalidPermission.Malformed")

	// Check that AuthorizeSecurityGroup adds the correct authorizations.
	_, err = s.ec2.AuthorizeSecurityGroup(g0, []ec2.IPPerm{{
		Protocol:  "tcp",
		FromPort:  2000,
		ToPort:    2001,
		SourceIPs: []string{"127.0.0.0/24"},
		SourceGroups: []ec2.UserSecurityGroup{{
			Name: g1.Name,
		}, {
			Id: g0.Id,
		}},
	}, {
		Protocol:  "tcp",
		FromPort:  2000,
		ToPort:    2001,
		SourceIPs: []string{"200.1.1.34/32"},
	}})
	c.Assert(err, IsNil)

	resp, err = s.ec2.SecurityGroups([]ec2.SecurityGroup{g0}, nil)
	c.Assert(err, IsNil)
	c.Assert(resp.Groups, HasLen, 1)
	c.Assert(resp.Groups[0].IPPerms, HasLen, 1)

	perm := resp.Groups[0].IPPerms[0]
	srcg := perm.SourceGroups
	c.Assert(srcg, HasLen, 2)

	// Normalize so we don't care about returned order.
	if srcg[0].Name == g1.Name {
		srcg[0], srcg[1] = srcg[1], srcg[0]
	}
	c.Check(srcg[0].Name, Equals, g0.Name)
	c.Check(srcg[0].Id, Equals, g0.Id)
	c.Check(srcg[0].OwnerId, Equals, ownerId)
	c.Check(srcg[1].Name, Equals, g1.Name)
	c.Check(srcg[1].Id, Equals, g1.Id)
	c.Check(srcg[1].OwnerId, Equals, ownerId)

	sort.Strings(perm.SourceIPs)
	c.Check(perm.SourceIPs, DeepEquals, []string{"127.0.0.0/24", "200.1.1.34/32"})

	// Check that we can't delete g1 (because g0 is using it)
	_, err = s.ec2.DeleteSecurityGroup(g1)
	c.Assert(err, NotNil)
	c.Check(errorCode(err), Equals, "InvalidGroup.InUse")

	_, err = s.ec2.RevokeSecurityGroup(g0, []ec2.IPPerm{{
		Protocol:     "tcp",
		FromPort:     2000,
		ToPort:       2001,
		SourceGroups: []ec2.UserSecurityGroup{{Id: g1.Id}},
	}, {
		Protocol:  "tcp",
		FromPort:  2000,
		ToPort:    2001,
		SourceIPs: []string{"200.1.1.34/32"},
	}})
	c.Assert(err, IsNil)

	resp, err = s.ec2.SecurityGroups([]ec2.SecurityGroup{g0}, nil)
	c.Assert(err, IsNil)
	c.Assert(resp.Groups, HasLen, 1)
	c.Assert(resp.Groups[0].IPPerms, HasLen, 1)

	perm = resp.Groups[0].IPPerms[0]
	srcg = perm.SourceGroups
	c.Assert(srcg, HasLen, 1)
	c.Check(srcg[0].Name, Equals, g0.Name)
	c.Check(srcg[0].Id, Equals, g0.Id)
	c.Check(srcg[0].OwnerId, Equals, ownerId)

	c.Check(perm.SourceIPs, DeepEquals, []string{"127.0.0.0/24"})

	// We should be able to delete g1 now because we've removed its only use.
	_, err = s.ec2.DeleteSecurityGroup(g1)
	c.Assert(err, IsNil)

	_, err = s.ec2.DeleteSecurityGroup(g0)
	c.Assert(err, IsNil)

	f := ec2.NewFilter()
	f.Add("group-id", g0.Id, g1.Id)
	resp, err = s.ec2.SecurityGroups(nil, f)
	c.Assert(err, IsNil)
	c.Assert(resp.Groups, HasLen, 0)
}

func (s *ServerTests) TestDuplicateIPPerm(c *C) {
	name := "goamz-test"
	descr := "goamz security group for tests"

	// Clean it up, if a previous test left it around and avoid leaving it around.
	s.ec2.DeleteSecurityGroup(ec2.SecurityGroup{Name: name})
	defer s.ec2.DeleteSecurityGroup(ec2.SecurityGroup{Name: name})

	resp1, err := s.ec2.CreateSecurityGroup(name, descr)
	c.Assert(err, IsNil)
	c.Assert(resp1.Name, Equals, name)

	perms := []ec2.IPPerm{{
		Protocol:  "tcp",
		FromPort:  200,
		ToPort:    1024,
		SourceIPs: []string{"127.0.0.1/24"},
	}, {
		Protocol:  "tcp",
		FromPort:  0,
		ToPort:    100,
		SourceIPs: []string{"127.0.0.1/24"},
	}}

	_, err = s.ec2.AuthorizeSecurityGroup(ec2.SecurityGroup{Name: name}, perms[0:1])
	c.Assert(err, IsNil)

	_, err = s.ec2.AuthorizeSecurityGroup(ec2.SecurityGroup{Name: name}, perms[0:2])
	c.Assert(errorCode(err), Equals, "InvalidPermission.Duplicate")
}

type filterSpec struct {
	name   string
	values []string
}

func (s *ServerTests) TestInstanceFiltering(c *C) {
	vpcResp, err := s.ec2.CreateVPC("10.4.0.0/16", "")
	c.Assert(err, IsNil)
	vpcId := vpcResp.VPC.Id
	defer s.deleteVPCs(c, []string{vpcId})

	subResp := s.createSubnet(c, vpcId, "10.4.1.0/24", "")
	subId := subResp.Subnet.Id
	defer s.deleteSubnets(c, []string{subId})

	groupResp, err := s.ec2.CreateSecurityGroup(
		sessionName("testgroup1"),
		"testgroup one description",
	)
	c.Assert(err, IsNil)
	group1 := groupResp.SecurityGroup

	groupResp, err = s.ec2.CreateSecurityGroupVPC(
		vpcId,
		sessionName("testgroup2"),
		"testgroup two description vpc",
	)
	c.Assert(err, IsNil)
	group2 := groupResp.SecurityGroup

	defer s.deleteGroups(c, []ec2.SecurityGroup{group1, group2})

	insts := make([]*ec2.Instance, 3)
	inst, err := s.ec2.RunInstances(&ec2.RunInstances{
		MinCount:       2,
		ImageId:        imageId,
		InstanceType:   "t1.micro",
		SecurityGroups: []ec2.SecurityGroup{group1},
	})
	c.Assert(err, IsNil)
	insts[0] = &inst.Instances[0]
	insts[1] = &inst.Instances[1]

	imageId2 := "ami-e358958a" // Natty server, i386, EBS store
	inst, err = s.ec2.RunInstances(&ec2.RunInstances{
		ImageId:        imageId2,
		InstanceType:   "t1.micro",
		SubnetId:       subId,
		SecurityGroups: []ec2.SecurityGroup{group2},
	})
	c.Assert(err, IsNil)
	insts[2] = &inst.Instances[0]

	ids := func(indices ...int) (instIds []string) {
		for _, index := range indices {
			instIds = append(instIds, insts[index].InstanceId)
		}
		return
	}

	defer terminateInstances(c, s.ec2, ids(0, 1, 2))

	tests := []struct {
		about       string
		instanceIds []string     // instanceIds argument to Instances method.
		filters     []filterSpec // filters argument to Instances method.
		resultIds   []string     // set of instance ids of expected results.
		allowExtra  bool         // resultIds may be incomplete.
		err         string       // expected error.
	}{
		{
			about:      "check that Instances returns all instances",
			resultIds:  ids(0, 1, 2),
			allowExtra: true,
		}, {
			about:       "check that specifying two instance ids returns them",
			instanceIds: ids(0, 2),
			resultIds:   ids(0, 2),
		}, {
			about:       "check that specifying a non-existent instance id gives an error",
			instanceIds: append(ids(0), "i-deadbeef"),
			err:         `.*\(InvalidInstanceID\.NotFound\)`,
		}, {
			about: "check that a filter allowed both instances returns both of them",
			filters: []filterSpec{
				{"instance-id", ids(0, 2)},
			},
			resultIds: ids(0, 2),
		}, {
			about: "check that a filter allowing only one instance returns it",
			filters: []filterSpec{
				{"instance-id", ids(1)},
			},
			resultIds: ids(1),
		}, {
			about: "check that a filter allowing no instances returns none",
			filters: []filterSpec{
				{"instance-id", []string{"i-deadbeef12345"}},
			},
		}, {
			about: "check that filtering on group id works",
			filters: []filterSpec{
				{"group-id", []string{group1.Id}},
			},
			resultIds: ids(0, 1),
		}, {
			about: "check that filtering on group id with instance prefix works",
			filters: []filterSpec{
				{"instance.group-id", []string{group1.Id}},
			},
			resultIds: ids(0, 1),
		}, {
			about: "check that filtering on group name works",
			filters: []filterSpec{
				{"group-name", []string{group1.Name}},
			},
			resultIds: ids(0, 1),
		}, {
			about: "check that filtering on group name with instance prefix works",
			filters: []filterSpec{
				{"instance.group-name", []string{group1.Name}},
			},
			resultIds: ids(0, 1),
		}, {
			about: "check that filtering on image id works",
			filters: []filterSpec{
				{"image-id", []string{imageId}},
			},
			resultIds:  ids(0, 1),
			allowExtra: true,
		}, {
			about: "combination filters 1",
			filters: []filterSpec{
				{"image-id", []string{imageId, imageId2}},
				{"group-name", []string{group1.Name}},
			},
			resultIds: ids(0, 1),
		}, {
			about: "combination filters 2",
			filters: []filterSpec{
				{"image-id", []string{imageId2}},
				{"group-name", []string{group1.Name}},
			},
		}, {
			about: "VPC filters in combination",
			filters: []filterSpec{
				{"vpc-id", []string{vpcId}},
				{"subnet-id", []string{subId}},
			},
			resultIds: ids(2),
		},
	}
	for i, t := range tests {
		c.Logf("%d. %s", i, t.about)
		var f *ec2.Filter
		if t.filters != nil {
			f = ec2.NewFilter()
			for _, spec := range t.filters {
				f.Add(spec.name, spec.values...)
			}
		}
		resp, err := s.ec2.Instances(t.instanceIds, f)
		if t.err != "" {
			c.Check(err, ErrorMatches, t.err)
			continue
		}
		c.Assert(err, IsNil)
		insts := make(map[string]*ec2.Instance)
		for _, r := range resp.Reservations {
			for j := range r.Instances {
				inst := &r.Instances[j]
				c.Check(insts[inst.InstanceId], IsNil, Commentf("duplicate instance id: %q", inst.InstanceId))
				insts[inst.InstanceId] = inst
			}
		}
		if !t.allowExtra {
			c.Check(insts, HasLen, len(t.resultIds), Commentf("expected %d instances got %#v", len(t.resultIds), insts))
		}
		for j, id := range t.resultIds {
			c.Check(insts[id], NotNil, Commentf("instance id %d (%q) not found; got %#v", j, id, insts))
		}
	}
}

func (s *AmazonServerSuite) TestRunInstancesVPC(c *C) {
	vpcResp, err := s.ec2.CreateVPC("10.6.0.0/16", "")
	c.Assert(err, IsNil)
	vpcId := vpcResp.VPC.Id
	defer s.deleteVPCs(c, []string{vpcId})

	subResp := s.createSubnet(c, vpcId, "10.6.1.0/24", "")
	subId := subResp.Subnet.Id
	defer s.deleteSubnets(c, []string{subId})

	groupResp, err := s.ec2.CreateSecurityGroupVPC(
		vpcId,
		sessionName("testgroup1 vpc"),
		"testgroup description vpc",
	)
	c.Assert(err, IsNil)
	group := groupResp.SecurityGroup

	defer s.deleteGroups(c, []ec2.SecurityGroup{group})

	// Run a single instance with a new network interface.
	ips := []ec2.PrivateIP{
		{Address: "10.6.1.10", IsPrimary: true},
		{Address: "10.6.1.20", IsPrimary: false},
	}
	instResp, err := s.ec2.RunInstances(&ec2.RunInstances{
		MinCount:     1,
		ImageId:      imageId,
		InstanceType: "t1.micro",
		NetworkInterfaces: []ec2.RunNetworkInterface{{
			DeviceIndex:         0,
			SubnetId:            subId,
			PrivateIPs:          ips,
			SecurityGroupIds:    []string{group.Id},
			DeleteOnTermination: true,
		}},
	})
	c.Assert(err, IsNil)
	inst := &instResp.Instances[0]

	defer terminateInstances(c, s.ec2, []string{inst.InstanceId})

	// Now list the network interfaces and find ours.
	testAttempt := aws.AttemptStrategy{
		Total: 5 * time.Minute,
		Delay: 5 * time.Second,
	}
	f := ec2.NewFilter()
	f.Add("subnet-id", subId)
	var newNIC *ec2.NetworkInterface
	for a := testAttempt.Start(); a.Next(); {
		c.Logf("waiting for NIC to become available")
		listNICs, err := s.ec2.NetworkInterfaces(nil, f)
		if err != nil {
			c.Logf("retrying; NetworkInterfaces returned: %v", err)
			continue
		}
		for _, iface := range listNICs.Interfaces {
			c.Logf("found NIC %v", iface)
			if iface.Attachment.InstanceId == inst.InstanceId {
				c.Logf("instance %v new NIC appeared", inst.InstanceId)
				newNIC = &iface
				break
			}
		}
		if newNIC != nil {
			break
		}
	}
	if newNIC == nil {
		c.Fatalf("timeout while waiting for NIC to appear.")
	}
	c.Check(newNIC.Id, Matches, `^eni-[0-9a-f]+$`)
	c.Check(newNIC.SubnetId, Equals, subId)
	c.Check(newNIC.VPCId, Equals, vpcId)
	c.Check(newNIC.Status, Matches, `^(attaching|in-use)$`)
	c.Check(newNIC.PrivateIPAddress, Equals, ips[0].Address)
	c.Check(newNIC.PrivateIPs, DeepEquals, ips)
	c.Check(newNIC.Groups, HasLen, 1)
	c.Check(newNIC.Groups[0].Id, Equals, group.Id)
	c.Check(newNIC.Attachment.Status, Matches, `^(attaching|attached)$`)
	c.Check(newNIC.Attachment.DeviceIndex, Equals, 0)
	c.Check(newNIC.Attachment.DeleteOnTermination, Equals, true)
}

func idsOnly(gs []ec2.SecurityGroup) []ec2.SecurityGroup {
	for i := range gs {
		gs[i].Name = ""
	}
	return gs
}

func namesOnly(gs []ec2.SecurityGroup) []ec2.SecurityGroup {
	for i := range gs {
		gs[i].Id = ""
	}
	return gs
}

func (s *ServerTests) TestGroupFiltering(c *C) {
	vpcResp, err := s.ec2.CreateVPC("10.5.0.0/16", "")
	c.Assert(err, IsNil)
	vpcId := vpcResp.VPC.Id
	defer s.deleteVPCs(c, []string{vpcId})

	subResp := s.createSubnet(c, vpcId, "10.5.1.0/24", "")
	subId := subResp.Subnet.Id
	defer s.deleteSubnets(c, []string{subId})

	g := make([]ec2.SecurityGroup, 5)
	for i := range g {
		var resp *ec2.CreateSecurityGroupResp
		gid := sessionName(fmt.Sprintf("testgroup%d", i))
		desc := fmt.Sprintf("testdescription%d", i)
		if i == 0 {
			// Create the first one as a VPC group.
			gid += " vpc"
			desc += " vpc"
			resp, err = s.ec2.CreateSecurityGroupVPC(vpcId, gid, desc)
		} else {
			resp, err = s.ec2.CreateSecurityGroup(gid, desc)
		}
		c.Assert(err, IsNil)
		g[i] = resp.SecurityGroup
		c.Logf("group %d: %v", i, g[i])
	}
	// Reorder the groups below, so that g[3] is first (some of the
	// reset depend on it, so they can't be deleted before g[3]). A
	// slight optimization for local live tests, so that we don't need
	// to wait 5s each time deleteGroups runs.
	defer s.deleteGroups(c, []ec2.SecurityGroup{g[3], g[0], g[1], g[2], g[4]})

	perms := [][]ec2.IPPerm{
		{{
			Protocol:  "tcp",
			FromPort:  100,
			ToPort:    200,
			SourceIPs: []string{"1.2.3.4/32"},
		}},
		{{
			Protocol:     "tcp",
			FromPort:     200,
			ToPort:       300,
			SourceGroups: []ec2.UserSecurityGroup{{Id: g[2].Id}},
		}},
		{{
			Protocol:     "udp",
			FromPort:     200,
			ToPort:       400,
			SourceGroups: []ec2.UserSecurityGroup{{Id: g[2].Id}},
		}},
	}
	for i, ps := range perms {
		_, err := s.ec2.AuthorizeSecurityGroup(g[i+1], ps)
		c.Assert(err, IsNil)
	}

	groups := func(indices ...int) (gs []ec2.SecurityGroup) {
		for _, index := range indices {
			gs = append(gs, g[index])
		}
		return
	}

	type groupTest struct {
		about      string
		groups     []ec2.SecurityGroup // groupIds argument to SecurityGroups method.
		filters    []filterSpec        // filters argument to SecurityGroups method.
		results    []ec2.SecurityGroup // set of expected result groups.
		allowExtra bool                // specified results may be incomplete.
		err        string              // expected error.
	}
	filterCheck := func(name, val string, gs []ec2.SecurityGroup) groupTest {
		return groupTest{
			about:      "filter check " + name,
			filters:    []filterSpec{{name, []string{val}}},
			results:    gs,
			allowExtra: true,
		}
	}
	tests := []groupTest{
		{
			about:      "check that SecurityGroups returns all groups",
			results:    groups(0, 1, 2, 3, 4),
			allowExtra: true,
		}, {
			about:   "check that specifying two group ids returns them",
			groups:  idsOnly(groups(0, 2)),
			results: groups(0, 2),
		}, {
			about:   "check that specifying names only works",
			groups:  namesOnly(groups(1, 2)),
			results: groups(1, 2),
		}, {
			about:  "check that specifying a non-existent group id gives an error",
			groups: append(groups(0), ec2.SecurityGroup{Id: "sg-eeeeeeeee"}),
			err:    `.*\(InvalidGroup\.NotFound\)`,
		}, {
			about: "check that a filter allowed two groups returns both of them",
			filters: []filterSpec{
				{"group-id", []string{g[0].Id, g[2].Id}},
			},
			results: groups(0, 2),
		},
		{
			about:  "check that the previous filter works when specifying a list of ids",
			groups: groups(1, 2),
			filters: []filterSpec{
				{"group-id", []string{g[0].Id, g[2].Id}},
			},
			results: groups(2),
		}, {
			about: "check that a filter allowing no groups returns none",
			filters: []filterSpec{
				{"group-id", []string{"sg-eeeeeeeee"}},
			},
		},
		filterCheck("description", "testdescription1", groups(1)),
		filterCheck("group-name", g[2].Name, groups(2)),
		filterCheck("ip-permission.cidr", "1.2.3.4/32", groups(1)),
		filterCheck("ip-permission.group-name", g[2].Name, groups(2, 3)),
		filterCheck("ip-permission.protocol", "udp", groups(3)),
		filterCheck("ip-permission.from-port", "200", groups(2, 3)),
		filterCheck("ip-permission.to-port", "200", groups(1)),
		filterCheck("vpc-id", vpcId, groups(0)),
		// TODO owner-id
	}
	for i, t := range tests {
		c.Logf("%d. %s", i, t.about)
		var f *ec2.Filter
		if t.filters != nil {
			f = ec2.NewFilter()
			for _, spec := range t.filters {
				f.Add(spec.name, spec.values...)
			}
		}
		resp, err := s.ec2.SecurityGroups(t.groups, f)
		if t.err != "" {
			c.Check(err, ErrorMatches, t.err)
			continue
		}
		c.Assert(err, IsNil)
		groups := make(map[string]*ec2.SecurityGroup)
		for j := range resp.Groups {
			group := &resp.Groups[j].SecurityGroup
			c.Check(groups[group.Id], IsNil, Commentf("duplicate group id: %q", group.Id))

			groups[group.Id] = group
		}
		// If extra groups may be returned, eliminate all groups that
		// we did not create in this session apart from the default group.
		if t.allowExtra {
			namePat := regexp.MustCompile(sessionName("testgroup[0-9]"))
			for id, g := range groups {
				if !namePat.MatchString(g.Name) {
					delete(groups, id)
				}
			}
		}
		c.Check(groups, HasLen, len(t.results))
		for j, g := range t.results {
			rg := groups[g.Id]
			c.Assert(rg, NotNil, Commentf("group %d (%v) not found; got %#v", j, g, groups))
			c.Check(rg.Name, Equals, g.Name, Commentf("group %d (%v)", j, g))
		}
	}
}

// deleteGroups ensures the given groups are deleted, by retrying
// until a timeout or all groups cannot be found anymore.
// This should be used to make sure tests leave no groups around.
func (s *ServerTests) deleteGroups(c *C, groups []ec2.SecurityGroup) {
	testAttempt := aws.AttemptStrategy{
		Total: 2 * time.Minute,
		Delay: 5 * time.Second,
	}
	for a := testAttempt.Start(); a.Next(); {
		deleted := 0
		c.Logf("deleting groups %v", groups)
		for _, group := range groups {
			_, err := s.ec2.DeleteSecurityGroup(group)
			if err == nil || errorCode(err) == "InvalidGroup.NotFound" {
				c.Logf("group %v deleted", group)
				deleted++
				continue
			}
			if err != nil {
				c.Logf("retrying; DeleteSecurityGroup returned: %v", err)
			}
		}
		if deleted == len(groups) {
			c.Logf("all groups deleted")
			return
		}
	}
	c.Fatalf("timeout while waiting %v groups to get deleted!", groups)
}

// errorCode returns the code of the given error, assuming it's not
// nil and it's an instance of *ec2.Error. It returns an empty string
// otherwise.
func errorCode(err error) string {
	if err, _ := err.(*ec2.Error); err != nil {
		return err.Code
	}
	return ""
}
