package zfs

import (
	"io/ioutil"
	"math"
	"os"
	"path/filepath"
	"testing"

	. "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
	gzfs "github.com/flynn/flynn/Godeps/_workspace/src/github.com/mistifyio/go-zfs"
	"github.com/flynn/flynn/pkg/testutils"
)

func Test(t *testing.T) { TestingT(t) }

type ZfsSnapshotTests struct {
	zpoolBackingFile *os.File
	zpool            *gzfs.Zpool
}

var _ = Suite(&ZfsSnapshotTests{})

func (ZfsSnapshotTests) SetUpSuite(c *C) {
	// Skip all tests in this suite if not running as root.
	// Many zfs operations require root priviledges.
	testutils.SkipIfNotRoot(c)
}

func (s *ZfsSnapshotTests) SetUpTest(c *C) {
	// Set up a temporary zpool.
	var err error
	s.zpoolBackingFile, err = ioutil.TempFile("/tmp/", "zfs-")
	c.Assert(err, IsNil)
	err = s.zpoolBackingFile.Truncate(int64(math.Pow(2, float64(30))))
	c.Assert(err, IsNil)
	s.zpool, err = gzfs.CreateZpool(
		"testpool",
		nil,
		"-mnone", // do not mount the root dataset.  (we'll mount our own datasets as necessary.)
		s.zpoolBackingFile.Name(),
	)
	c.Assert(err, IsNil)
}

func (s *ZfsSnapshotTests) TearDownTest(c *C) {
	if s.zpoolBackingFile != nil {
		s.zpoolBackingFile.Close()
		os.Remove(s.zpoolBackingFile.Name())
		if s.zpool != nil {
			err := s.zpool.Destroy()
			c.Assert(err, IsNil)
		}
	}
}

func (ZfsSnapshotTests) TestSnapshotShouldCarryFiles(c *C) {
	provider, err := NewProvider(&ProviderConfig{DatasetName: "testpool"})
	c.Assert(err, IsNil)

	v, err := provider.NewVolume()
	c.Assert(err, IsNil)

	// a new volume should start out empty:
	c.Assert(v.Location(), testutils.DirContains, []string{})

	f, err := os.Create(filepath.Join(v.Location(), "alpha"))
	c.Assert(err, IsNil)
	f.Close()

	// sanity check, can we so much as even write a file:
	c.Assert(v.Location(), testutils.DirContains, []string{"alpha"})

	v2, err := v.TakeSnapshot()
	c.Assert(err, IsNil)

	// taking a snapshot shouldn't change the source dir:
	c.Assert(v.Location(), testutils.DirContains, []string{"alpha"})
	// the newly mounted snapshot in the new location should have the same content:
	c.Assert(v2.Location(), testutils.DirContains, []string{"alpha"})
}

func (ZfsSnapshotTests) TestSnapshotShouldIsolateNewChangesToSource(c *C) {
	provider, err := NewProvider(&ProviderConfig{DatasetName: "testpool"})
	c.Assert(err, IsNil)

	v, err := provider.NewVolume()
	c.Assert(err, IsNil)

	// a new volume should start out empty:
	c.Assert(v.Location(), testutils.DirContains, []string{})

	f, err := os.Create(filepath.Join(v.Location(), "alpha"))
	c.Assert(err, IsNil)
	f.Close()

	// sanity check, can we so much as even write a file:
	c.Assert(v.Location(), testutils.DirContains, []string{"alpha"})

	v2, err := v.TakeSnapshot()
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

func (ZfsSnapshotTests) TestSnapshotShouldIsolateNewChangesToFork(c *C) {
	provider, err := NewProvider(&ProviderConfig{DatasetName: "testpool"})
	c.Assert(err, IsNil)

	v, err := provider.NewVolume()
	c.Assert(err, IsNil)

	// a new volume should start out empty:
	c.Assert(v.Location(), testutils.DirContains, []string{})

	f, err := os.Create(filepath.Join(v.Location(), "alpha"))
	c.Assert(err, IsNil)
	f.Close()

	// sanity check, can we so much as even write a file:
	c.Assert(v.Location(), testutils.DirContains, []string{"alpha"})

	v2, err := v.TakeSnapshot()
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
