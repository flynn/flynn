package zfs

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/flynn/flynn/pkg/testutils"
	. "github.com/flynn/go-check"
)

type ZfsTransmitTests struct {
	pool1 TempZpool
	pool2 TempZpool
}

var _ = Suite(&ZfsTransmitTests{})

func (ZfsTransmitTests) SetUpSuite(c *C) {
	// Skip all tests in this suite if not running as root.
	// Many zfs operations require root priviledges.
	testutils.SkipIfNotRoot(c)
}

func (s *ZfsTransmitTests) SetUpTest(c *C) {
	s.pool1.SetUpTest(c)
	s.pool2.SetUpTest(c)
}

func (s *ZfsTransmitTests) TearDownTest(c *C) {
	s.pool1.TearDownTest(c)
	s.pool2.TearDownTest(c)
}

/*
	Testing behaviors of 'zfs send' & 'zfs recv' in isolation to make sure deltas work the way we expect.

	See integration tests for taking the full trip over the wire through the REST API.
*/
func (s *ZfsTransmitTests) TestZfsSendRecvFull(c *C) {
	// create volume; add content; snapshot it.
	// note that 'zfs send' refuses anything but snapshots.
	v, err := s.pool1.VolProv.NewVolume(nil)
	c.Assert(err, IsNil)
	f, err := os.Create(filepath.Join(v.Location(), "alpha"))
	c.Assert(err, IsNil)
	f.Close()
	snap, err := s.pool1.VolProv.CreateSnapshot(v)
	c.Assert(err, IsNil)

	var buf bytes.Buffer
	s.pool1.VolProv.SendSnapshot(snap, nil, &buf)

	// send stream to this pool; should get a new snapshot volume
	v2, err := s.pool1.VolProv.NewVolume(nil)
	c.Assert(err, IsNil)
	snapRestored, err := s.pool1.VolProv.ReceiveSnapshot(v2, bytes.NewBuffer(buf.Bytes()))
	c.Assert(err, IsNil)
	c.Assert(snapRestored.IsSnapshot(), Equals, true)
	// check that contents came across in the snapshot
	c.Assert(snapRestored.Location(), testutils.DirContains, []string{"alpha"})
	// check that the contents applied to the volume
	c.Assert(v2.Location(), testutils.DirContains, []string{"alpha"})

	// send stream to another pool; should get a new volume
	v2, err = s.pool2.VolProv.NewVolume(nil)
	snapRestored, err = s.pool2.VolProv.ReceiveSnapshot(v2, bytes.NewBuffer(buf.Bytes()))
	c.Assert(err, IsNil)
	c.Assert(snapRestored.IsSnapshot(), Equals, true)
	// check that contents came across in the snapshot
	c.Assert(snapRestored.Location(), testutils.DirContains, []string{"alpha"})
	// check that the contents applied to the volume
	c.Assert(v2.Location(), testutils.DirContains, []string{"alpha"})
}

/*
	Test that sending incremental deltas works (and is smaller than wholes).
*/
func (s *ZfsTransmitTests) TestZfsSendRecvIncremental(c *C) {
	// create volume; add content; snapshot it.
	v, err := s.pool1.VolProv.NewVolume(nil)
	c.Assert(err, IsNil)
	f, err := os.Create(filepath.Join(v.Location(), "alpha"))
	c.Assert(err, IsNil)
	f.Close()
	snap, err := s.pool1.VolProv.CreateSnapshot(v)
	c.Assert(err, IsNil)

	var buf bytes.Buffer
	s.pool1.VolProv.SendSnapshot(snap, nil, &buf)
	fmt.Printf("note: size of snapshot stream is %d bytes\n", buf.Len()) // 41680

	// send stream to another pool; should get a new snapshot volume
	v2, err := s.pool2.VolProv.NewVolume(nil)
	c.Assert(err, IsNil)
	snapRestored1, err := s.pool2.VolProv.ReceiveSnapshot(v2, bytes.NewBuffer(buf.Bytes()))
	c.Assert(err, IsNil)
	c.Assert(snapRestored1.IsSnapshot(), Equals, true)
	// check that contents came across in the snapshot
	c.Assert(snapRestored1.Location(), testutils.DirContains, []string{"alpha"})
	// check that the contents applied to the volume
	c.Assert(v2.Location(), testutils.DirContains, []string{"alpha"})

	// edit files; make another snapshot
	f, err = os.Create(filepath.Join(v.Location(), "beta"))
	c.Assert(err, IsNil)
	f.Close()
	snap2, err := s.pool1.VolProv.CreateSnapshot(v)
	c.Assert(err, IsNil)

	// make another complete snapshot, just to check size
	buf.Reset()
	s.pool1.VolProv.SendSnapshot(snap2, nil, &buf)
	fmt.Printf("note: size of bigger snapshot stream is %d bytes\n", buf.Len()) // 42160

	// form an incremental snapshot by asking the recipient what it has, and telling the sender to go ahead with that.
	// this mirrors what the API would do when the recipient initiates a pull in the fully integrated system.
	buf.Reset()
	haves, err := s.pool2.VolProv.ListHaves(v2)
	c.Assert(err, IsNil)
	err = s.pool1.VolProv.SendSnapshot(snap2, haves, &buf)
	c.Assert(err, IsNil)
	fmt.Printf("note: size of incremental stream is %d bytes\n", buf.Len()) // 10064

	// receive should work pretty much the same.
	snapRestored2, err := s.pool2.VolProv.ReceiveSnapshot(v2, bytes.NewBuffer(buf.Bytes()))
	c.Assert(err, IsNil)
	c.Assert(snapRestored2.IsSnapshot(), Equals, true)
	// check that contents came across in the snapshot
	c.Assert(snapRestored2.Location(), testutils.DirContains, []string{"alpha", "beta"})
	// check that the contents applied to the volume
	c.Assert(v2.Location(), testutils.DirContains, []string{"alpha", "beta"})
}
