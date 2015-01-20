package zfs

import (
	"os"
	"path/filepath"
	"testing"

	. "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
)

func Test(t *testing.T) { TestingT(t) }

type S struct{}

var _ = Suite(&S{})

func (S) SetUpSuite(c *C) {
	// Skip all tests in this suite if not running as root.
	// Many zfs operations require root priviledges.
	skipIfNotRoot(c)
}

func (S) TestSnapshotShouldCarryFiles(c *C) {
	err := WithTmpfileZpool("testpool", func() error {
		provider, err := NewProvider(&ProviderConfig{DatasetName: "testpool"})
		if err != nil {
			return err
		}

		v, err := provider.NewVolume()
		if err != nil {
			return err
		}

		// a new volume should start out empty:
		c.Assert(v.(*zfsVolume).basemount, DirContains, []string{})

		f, err := os.Create(filepath.Join(v.(*zfsVolume).basemount, "alpha"))
		c.Assert(err, IsNil)
		f.Close()

		// sanity check, can we so much as even write a file:
		c.Assert(v.(*zfsVolume).basemount, DirContains, []string{"alpha"})

		v2, err := v.TakeSnapshot()
		if err != nil {
			return err
		}

		// taking a snapshot shouldn't change the source dir:
		c.Assert(v.(*zfsVolume).basemount, DirContains, []string{"alpha"})
		// the newly mounted snapshot in the new location should have the same content:
		c.Assert(v2.(*zfsVolume).basemount, DirContains, []string{"alpha"})

		return nil
	})
	c.Assert(err, IsNil)
}

func (S) TestSnapshotShouldIsolateNewChangesToSource(c *C) {
	err := WithTmpfileZpool("testpool", func() error {
		provider, err := NewProvider(&ProviderConfig{DatasetName: "testpool"})
		if err != nil {
			return err
		}

		v, err := provider.NewVolume()
		if err != nil {
			return err
		}

		// a new volume should start out empty:
		c.Assert(v.(*zfsVolume).basemount, DirContains, []string{})

		f, err := os.Create(filepath.Join(v.(*zfsVolume).basemount, "alpha"))
		c.Assert(err, IsNil)
		f.Close()

		// sanity check, can we so much as even write a file:
		c.Assert(v.(*zfsVolume).basemount, DirContains, []string{"alpha"})

		v2, err := v.TakeSnapshot()
		if err != nil {
			return err
		}

		// write another file to the source
		f, err = os.Create(filepath.Join(v.(*zfsVolume).basemount, "beta"))
		c.Assert(err, IsNil)
		f.Close()

		// the source dir should contain our changes:
		c.Assert(v.(*zfsVolume).basemount, DirContains, []string{"alpha", "beta"})
		// the snapshot should be unaffected:
		c.Assert(v2.(*zfsVolume).basemount, DirContains, []string{"alpha"})

		return nil
	})
	c.Assert(err, IsNil)
}

func (S) TestSnapshotShouldIsolateNewChangesToFork(c *C) {
	err := WithTmpfileZpool("testpool", func() error {
		provider, err := NewProvider(&ProviderConfig{DatasetName: "testpool"})
		if err != nil {
			return err
		}

		v, err := provider.NewVolume()
		if err != nil {
			return err
		}

		// a new volume should start out empty:
		c.Assert(v.(*zfsVolume).basemount, DirContains, []string{})

		f, err := os.Create(filepath.Join(v.(*zfsVolume).basemount, "alpha"))
		c.Assert(err, IsNil)
		f.Close()

		// sanity check, can we so much as even write a file:
		c.Assert(v.(*zfsVolume).basemount, DirContains, []string{"alpha"})

		v2, err := v.TakeSnapshot()
		if err != nil {
			return err
		}

		// write another file to the fork
		f, err = os.Create(filepath.Join(v2.(*zfsVolume).basemount, "beta"))
		c.Assert(err, IsNil)
		f.Close()

		// the source dir should be unaffected:
		c.Assert(v.(*zfsVolume).basemount, DirContains, []string{"alpha"})
		// the snapshot should contain our changes:
		c.Assert(v2.(*zfsVolume).basemount, DirContains, []string{"alpha", "beta"})

		return nil
	})
	c.Assert(err, IsNil)
}
