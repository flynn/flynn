package main

import (
	"time"

	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/attempt"
	tc "github.com/flynn/flynn/test/cluster"
	c "github.com/flynn/go-check"
)

// Prefix the suite with "Z" so that it runs after all other tests
type ZDiscoverdSuite struct {
	Helper
}

var _ = c.Suite(&ZDiscoverdSuite{})

func (s *ZDiscoverdSuite) TestDeploy(t *c.C) {
	// ensure we have enough hosts in the cluster
	hosts, err := s.clusterClient(t).Hosts()
	t.Assert(err, c.IsNil)
	if len(hosts) <= 1 {
		t.Skip("cannot deploy discoverd in a single node cluster")
	}

	client := s.controllerClient(t)
	app, err := client.GetApp("discoverd")
	t.Assert(err, c.IsNil)
	release, err := client.GetAppRelease(app.ID)
	t.Assert(err, c.IsNil)
	release.ID = ""
	t.Assert(client.CreateRelease(release), c.IsNil)
	deployment, err := client.CreateDeployment(app.ID, release.ID)
	t.Assert(err, c.IsNil)

	events := make(chan *ct.DeploymentEvent)
	stream, err := client.StreamDeployment(deployment, events)
	t.Assert(err, c.IsNil)
	defer stream.Close()

loop:
	for {
		select {
		case event, ok := <-events:
			if !ok {
				t.Fatal("unexpected close of deployment event stream")
			}
			if event.Status == "complete" {
				debugf(t, "got deployment event: %s", event.Status)
				break loop
			}
			if event.Status == "failed" {
				t.Fatal("the deployment failed")
			}
			debugf(t, "got deployment event: %s %s", event.JobType, event.JobState)
		case <-time.After(time.Duration(app.DeployTimeout) * time.Second):
			t.Fatal("timed out waiting for deployment event")
		}
	}
}

func peerPresent(host *tc.Instance, peers []string) bool {
	present := false
	for _, p := range peers {
		if p == host.IP+":1111" {
			present = true
			break
		}
	}
	return present
}

var pingAttempts = attempt.Strategy{
	Min:   5,
	Total: time.Minute,
	Delay: time.Second,
}

func (s *ZDiscoverdSuite) TestPromoteDemote(t *c.C) {
	if testCluster == nil {
		t.Skip("cannot boot new hosts")
	}
	// ensure we have 3 node cluster, TODO(jpg): Support running test on anything larger than 2 node cluster
	hosts, err := s.clusterClient(t).Hosts()
	t.Assert(err, c.IsNil)
	if len(hosts) != 3 {
		t.Skip("promotion and demotion tests require a 3 node cluster")
	}

	// Check the original number of peers is correct
	initialPeers, err := s.discoverdClient(t).RaftPeers()
	t.Assert(err, c.IsNil)
	t.Assert(len(initialPeers), c.Equals, 3)

	// Add a new host to the cluster, initially it will join as a proxy
	newHost := s.addHost(t, "discoverd")
	defer s.removeHost(t, newHost, "discoverd")

	// Ping the new node until it comes up
	url := "http://" + newHost.IP + ":1111"
	dd := discoverd.NewClientWithURL(url)
	err = pingAttempts.Run(func() error { return dd.Ping(url) })
	t.Assert(err, c.IsNil)

	// Promote the new node to a Raft member
	err = dd.Promote(url)
	t.Assert(err, c.IsNil)

	// Check that we now have one additional peer, also ensure our new peer is in the list
	newPeers, err := s.discoverdClient(t).RaftPeers()
	t.Assert(err, c.IsNil)
	t.Assert(len(newPeers), c.Equals, 4)
	t.Assert(peerPresent(newHost, newPeers), c.Equals, true)

	// Now demote the newly promoted node
	err = dd.Demote(url)
	t.Assert(err, c.IsNil)

	//XXX(jpg): Better way to wait for leadership?
	time.Sleep(2 * time.Second)

	// We are going to ask the leader for the list of peers as it's definitely canonical
	leader, err := s.discoverdClient(t).RaftLeader()
	t.Assert(err, c.IsNil)
	dd = discoverd.NewClientWithURL(leader.Host)

	// There should now be only the original peers, additionally make sure our host isn't one of them
	finalPeers, err := dd.RaftPeers()
	t.Assert(err, c.IsNil)
	t.Assert(len(finalPeers), c.Equals, 3)
	t.Assert(peerPresent(newHost, finalPeers), c.Equals, false)
}
