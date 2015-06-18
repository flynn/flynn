package main

import (
	c "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/pkg/cluster"
)

type VolumeSuite struct {
	Helper
}

var _ = c.Suite(&VolumeSuite{})

func (s *VolumeSuite) TestVolumeTransmitAPI(t *c.C) {
	hosts, err := s.clusterClient(t).Hosts()
	t.Assert(err, c.IsNil)
	s.doVolumeTransmitAPI(hosts[0], hosts[0], t)
}

func (s *VolumeSuite) TestInterhostVolumeTransmitAPI(t *c.C) {
	hosts, err := s.clusterClient(t).Hosts()
	t.Assert(err, c.IsNil)
	if len(hosts) < 2 {
		t.Skip("need multiple hosts for this test")
	}
	s.doVolumeTransmitAPI(hosts[0], hosts[1], t)
}

func (s *VolumeSuite) doVolumeTransmitAPI(h0, h1 *cluster.Host, t *c.C) {
	clus := s.clusterClient(t)

	// create a volume!
	vol, err := h0.CreateVolume("default")
	t.Assert(err, c.IsNil)
	defer func() {
		t.Assert(h0.DestroyVolume(vol.ID), c.IsNil)
	}()
	// create a job and use it to add data to the volume
	cmd, service, err := makeIshApp(clus, h0, s.discoverdClient(t), host.ContainerConfig{
		Volumes: []host.VolumeBinding{{
			Target:    "/vol",
			VolumeID:  vol.ID,
			Writeable: true,
		}},
	})
	t.Assert(err, c.IsNil)
	defer cmd.Kill()
	resp, err := runIshCommand(service, "echo 'testcontent' > /vol/alpha ; echo $?")
	t.Assert(err, c.IsNil)
	t.Assert(resp, c.Equals, "0\n")

	// take a snapshot
	snapInfo, err := h0.CreateSnapshot(vol.ID)
	t.Assert(err, c.IsNil)
	defer func() {
		t.Assert(h0.DestroyVolume(snapInfo.ID), c.IsNil)
	}()
	// make a volume on another host to yank the snapshot content into
	vol2, err := h1.CreateVolume("default")
	t.Assert(err, c.IsNil)
	defer func() {
		t.Assert(h1.DestroyVolume(vol2.ID), c.IsNil)
	}()
	// transfer the snapshot to the new volume on the other host
	snapInfo2, err := h1.PullSnapshot(vol2.ID, h0.ID(), snapInfo.ID)
	t.Assert(err, c.IsNil)
	defer func() {
		t.Assert(h1.DestroyVolume(snapInfo2.ID), c.IsNil)
	}()

	// start a job on the other host that mounts and inspects the transmitted volume
	cmd, service, err = makeIshApp(clus, h1, s.discoverdClient(t), host.ContainerConfig{
		Volumes: []host.VolumeBinding{{
			Target:    "/vol",
			VolumeID:  vol2.ID,
			Writeable: false,
		}},
	})
	t.Assert(err, c.IsNil)
	defer cmd.Kill()
	// read data back from the volume
	resp, err = runIshCommand(service, "cat /vol/alpha")
	t.Assert(err, c.IsNil)
	t.Assert(resp, c.Equals, "testcontent\n")
}
