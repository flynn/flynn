package zfs

import (
	"os"
	"path/filepath"
	"syscall"

	"github.com/flynn/flynn/pkg/testutils"
	. "github.com/flynn/go-check"
)

type ZfsSnapshotTests struct {
	TempZpool
}

var _ = Suite(&ZfsSnapshotTests{})

func (ZfsSnapshotTests) SetUpSuite(c *C) {
	// Skip all tests in this suite if not running as root.
	// Many zfs operations require root priviledges.
	testutils.SkipIfNotRoot(c)
}

func (s *ZfsSnapshotTests) TestSnapshotShouldCarryFiles(c *C) {
	v, err := s.VolProv.NewVolume(nil)
	c.Assert(err, IsNil)

	// a new volume should start out empty:
	c.Assert(v.Location(), testutils.DirContains, []string{})
	c.Assert(v.IsSnapshot(), Equals, false)

	f, err := os.Create(filepath.Join(v.Location(), "alpha"))
	c.Assert(err, IsNil)
	f.Close()

	// sanity check, can we so much as even write a file:
	c.Assert(v.Location(), testutils.DirContains, []string{"alpha"})

	// create a volume.  check that it looks like one.
	v2, err := s.VolProv.CreateSnapshot(v)
	c.Assert(err, IsNil)
	c.Assert(v2.IsSnapshot(), Equals, true)

	// taking a snapshot shouldn't change the source dir:
	c.Assert(v.Location(), testutils.DirContains, []string{"alpha"})
	// the newly mounted snapshot in the new location should have the same content:
	c.Assert(v2.Location(), testutils.DirContains, []string{"alpha"})
}

func (s *ZfsSnapshotTests) TestSnapshotShouldIsolateNewChangesToSource(c *C) {
	v, err := s.VolProv.NewVolume(nil)
	c.Assert(err, IsNil)

	// a new volume should start out empty:
	c.Assert(v.Location(), testutils.DirContains, []string{})

	f, err := os.Create(filepath.Join(v.Location(), "alpha"))
	c.Assert(err, IsNil)
	f.Close()

	// sanity check, can we so much as even write a file:
	c.Assert(v.Location(), testutils.DirContains, []string{"alpha"})

	v2, err := s.VolProv.CreateSnapshot(v)
	c.Assert(err, IsNil)

	// write another file to the source
	f, err = os.Create(filepath.Join(v.Location(), "beta"))
	c.Assert(err, IsNil)
	f.Close()

	// the source dir should contain our changes:
	c.Assert(v.Location(), testutils.DirContains, []string{"alpha", "beta"})
	// the snapshot should be unaffected:
	c.Assert(v2.Location(), testutils.DirContains, []string{"alpha"})
}

func (s *ZfsSnapshotTests) TestSnapshotShouldBeReadOnly(c *C) {
	v, err := s.VolProv.NewVolume(nil)
	c.Assert(err, IsNil)

	// a new volume should start out empty:
	c.Assert(v.Location(), testutils.DirContains, []string{})

	f, err := os.Create(filepath.Join(v.Location(), "alpha"))
	c.Assert(err, IsNil)
	f.Close()

	// sanity check, can we so much as even write a file:
	c.Assert(v.Location(), testutils.DirContains, []string{"alpha"})

	v2, err := s.VolProv.CreateSnapshot(v)
	c.Assert(err, IsNil)

	// write another file to the snapshot; should fail
	f, err = os.Create(filepath.Join(v2.Location(), "beta"))
	f.Close()
	c.Assert(err, NotNil)
	c.Assert(err, FitsTypeOf, &os.PathError{})
	c.Assert(err.(*os.PathError).Err, Equals, syscall.EROFS)
}

func (s *ZfsSnapshotTests) TestForkedSnapshotShouldIsolateNewChangesToFork(c *C) {
	v, err := s.VolProv.NewVolume(nil)
	c.Assert(err, IsNil)

	// a new volume should start out empty:
	c.Assert(v.Location(), testutils.DirContains, []string{})

	f, err := os.Create(filepath.Join(v.Location(), "alpha"))
	c.Assert(err, IsNil)
	f.Close()

	// sanity check, can we so much as even write a file:
	c.Assert(v.Location(), testutils.DirContains, []string{"alpha"})

	snap, err := s.VolProv.CreateSnapshot(v)
	c.Assert(err, IsNil)
	v2, err := s.VolProv.ForkVolume(snap)
	c.Assert(err, IsNil)

	// write another file to the fork
	f, err = os.Create(filepath.Join(v2.Location(), "beta"))
	c.Assert(err, IsNil)
	f.Close()

	// the source dir should be unaffected:
	c.Assert(v.Location(), testutils.DirContains, []string{"alpha"})
	// the snapshot should contain our changes:
	c.Assert(v2.Location(), testutils.DirContains, []string{"alpha", "beta"})
}
