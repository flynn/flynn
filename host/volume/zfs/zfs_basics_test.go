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

/*
option 1
--------

apt-get install zfs-fuse


option 2
--------

gpg --keyserver pgp.mit.edu --recv-keys "F6B0FC61"
gpg --armor --export "F6B0FC61" | apt-key add -
echo deb http://ppa.launchpad.net/zfs-native/stable/ubuntu trusty main > /etc/apt/sources.list.d/zfs.list
apt-get update
apt-get install -y ubuntu-zfs

this drags in g++, and proceeds to spend a large number of seconds on "Building initial module for 3.13.0-43-generic"...
okay, minutes.  ugh.
*/

//

func (S) TestCoolerThings(c *C) {
	err := WithTmpfileZpool("testpool", func() error {
		provider, err := NewProvider("testpool")
		if err != nil {
			return err
		}

		v, err := provider.NewVolume()
		if err != nil {
			return err
		}

		// a new volume should start out empty:
		c.Assert(v.(*zfsVolume).basemount, DirContains, []string{})

		_, err = os.Create(filepath.Join(v.(*zfsVolume).basemount, "alpha"))
		c.Assert(err, IsNil)

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
