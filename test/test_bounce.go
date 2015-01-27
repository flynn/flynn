package main

import (
	c "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
	"github.com/flynn/flynn/host/types"
)

/*
	Sanity check over our CI system to make sure we can in fact bounce hosts,
	and that does what we think it does.

	Note that any tests that involve raising or destroying hosts that join the
	main test bootstrap cluster cannot be run concurrently.
*/
type BounceSuite struct {
	Helper
}

var _ = c.Suite(&BounceSuite{})

func (BounceSuite) SetUpSuite(t *c.C) {
	if testCluster == nil {
		t.Skip("cannot boot new hosts")
	}
}

func (s *BounceSuite) TestHostUpDown(t *c.C) {
	// get host event stream to watch
	ch := make(chan *host.HostEvent)
	stream, err := s.clusterClient(t).StreamHostEvents(ch)
	t.Assert(err, c.IsNil)
	defer stream.Close()

	// request a new host; use stream to await its availability
	instance, err := testCluster.AddHost(ch, false)
	t.Assert(err, c.IsNil)

	// destroy that host to clean up
	err = testCluster.RemoveHost(instance)
	t.Assert(err, c.IsNil)
}

func (s *BounceSuite) TestBounceHostVM(t *c.C) {
	// get host event stream to watch
	ch := make(chan *host.HostEvent)
	stream, err := s.clusterClient(t).StreamHostEvents(ch)
	t.Assert(err, c.IsNil)
	defer stream.Close()

	// request a new host; use stream to await its availability
	instance, err := testCluster.AddHost(ch, false)
	t.Assert(err, c.IsNil)

	// bounce that host
	// this includes VM reboot and then relaunching of the host daemon
	err = testCluster.BounceHost(instance)
	t.Assert(err, c.IsNil)

	// destroy that host to clean up
	err = testCluster.RemoveHost(instance)
	t.Assert(err, c.IsNil)
}
