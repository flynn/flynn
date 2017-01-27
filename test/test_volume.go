package main

import (
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/host/volume"
	"github.com/flynn/flynn/pkg/cluster"
	c "github.com/flynn/go-check"
)

type VolumeSuite struct {
	Helper
}

var _ = c.ConcurrentSuite(&VolumeSuite{})

func (s *VolumeSuite) TestVolumeTransmitAPI(t *c.C) {
	x := s.bootCluster(t, 1)
	defer x.Destroy()
	s.doVolumeTransmitAPI(t, x, x.Host.Host, x.Host.Host)
}

func (s *VolumeSuite) TestInterhostVolumeTransmitAPI(t *c.C) {
	x := s.bootCluster(t, 3)
	defer x.Destroy()
	var host1, host2 *cluster.Host
	for _, h := range x.Hosts {
		if host1 == nil {
			host1 = h.Host
		} else if host2 == nil {
			host2 = h.Host
		} else {
			break
		}
	}

	s.doVolumeTransmitAPI(t, x, host1, host2)
}

func (s *VolumeSuite) doVolumeTransmitAPI(t *c.C, x *Cluster, h0, h1 *cluster.Host) {
	// create a volume!
	vol := &volume.Info{}
	t.Assert(h0.CreateVolume("default", vol), c.IsNil)
	defer func() {
		t.Assert(h0.DestroyVolume(vol.ID), c.IsNil)
	}()
	// create a job and use it to add data to the volume
	ish, err := s.makeIshApp(t, &IshApp{cluster: x, host: h0, extraConfig: host.ContainerConfig{
		Volumes: []host.VolumeBinding{{
			Target:    "/vol",
			VolumeID:  vol.ID,
			Writeable: true,
		}},
	}})
	t.Assert(err, c.IsNil)
	defer ish.Cleanup()
	resp, err := ish.run("echo 'testcontent' > /vol/alpha ; echo $?")
	t.Assert(err, c.IsNil)
	t.Assert(resp, c.Equals, "0\n")

	// take a snapshot
	snapInfo, err := h0.CreateSnapshot(vol.ID)
	t.Assert(err, c.IsNil)
	defer func() {
		t.Assert(h0.DestroyVolume(snapInfo.ID), c.IsNil)
	}()
	// make a volume on another host to yank the snapshot content into
	vol2 := &volume.Info{}
	t.Assert(h1.CreateVolume("default", vol2), c.IsNil)
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
	ish, err = s.makeIshApp(t, &IshApp{cluster: x, host: h1, extraConfig: host.ContainerConfig{
		Volumes: []host.VolumeBinding{{
			Target:    "/vol",
			VolumeID:  vol2.ID,
			Writeable: false,
		}},
	}})
	t.Assert(err, c.IsNil)
	defer ish.Cleanup()
	// read data back from the volume
	resp, err = ish.run("cat /vol/alpha")
	t.Assert(err, c.IsNil)
	t.Assert(resp, c.Equals, "testcontent\n")
}
