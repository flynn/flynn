package volumemanager_test

import (
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	. "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
	gzfs "github.com/flynn/flynn/Godeps/_workspace/src/github.com/mistifyio/go-zfs"
	"github.com/flynn/flynn/host/volume"
	"github.com/flynn/flynn/host/volume/manager"
	"github.com/flynn/flynn/host/volume/zfs"
	"github.com/flynn/flynn/pkg/random"
	"github.com/flynn/flynn/pkg/testutils"
)

func Test(t *testing.T) { TestingT(t) }

// note: many of these tests are not zfs specific; refactoring this when we have more concrete backends will be wise.

type PersistenceTests struct{}

var _ = Suite(&PersistenceTests{})

func (PersistenceTests) SetUpSuite(c *C) {
	// Skip all tests in this suite if not running as root.
	// Many zfs operations require root priviledges.
	testutils.SkipIfNotRoot(c)
}

// covers basic volume persistence and named volume persistence
func (s *PersistenceTests) TestPersistence(c *C) {
	idString := random.String(12)
	vmanDBfilePath := fmt.Sprintf("/tmp/flynn-volumes-%s.bolt", idString)
	zfsDatasetName := fmt.Sprintf("flynn-test-dataset-%s", idString)
	zfsVdevFilePath := fmt.Sprintf("/tmp/flynn-test-zpool-%s.vdev", idString)
	defer os.Remove(vmanDBfilePath)
	defer os.Remove(zfsVdevFilePath)
	defer func() {
		pool, _ := gzfs.GetZpool(zfsDatasetName)
		if pool != nil {
			if datasets, err := pool.Datasets(); err == nil {
				for _, dataset := range datasets {
					dataset.Destroy(gzfs.DestroyRecursive | gzfs.DestroyForceUmount)
					os.Remove(dataset.Mountpoint)
				}
			}
			err := pool.Destroy()
			c.Assert(err, IsNil)
		}
	}()

	// new volume manager with a new backing zfs vdev file and a new boltdb
	volProv, err := zfs.NewProvider(&zfs.ProviderConfig{
		DatasetName: zfsDatasetName,
		Make: &zfs.MakeDev{
			BackingFilename: zfsVdevFilePath,
			Size:            int64(math.Pow(2, float64(30))),
		},
	})
	c.Assert(err, IsNil)

	// new volume manager with that shiny new backing zfs vdev file and a new boltdb
	vman, err := volumemanager.New(
		vmanDBfilePath,
		func() (volume.Provider, error) { return volProv, nil },
	)
	c.Assert(err, IsNil)

	// make a volume
	vol1, err := vman.NewVolume()
	c.Assert(err, IsNil)

	// make a named volume
	vol2, err := vman.CreateOrGetNamedVolume("aname", "")
	c.Assert(err, IsNil)

	// assert existence of filesystems; emplace some data
	f, err := os.Create(filepath.Join(vol1.Location(), "alpha"))
	c.Assert(err, IsNil)
	f.Close()
	f, err = os.Create(filepath.Join(vol2.Location(), "beta"))
	c.Assert(err, IsNil)
	f.Close()

	// close persistence
	c.Assert(vman.PersistenceDBClose(), IsNil)

	// hack zfs export/umounting to emulate host shutdown
	err = exec.Command("zpool", "export", "-f", zfsDatasetName).Run()
	c.Assert(err, IsNil)

	// sanity check: assert the filesystems are gone
	// note that the directories remain present after 'zpool export'
	_, err = os.Stat(filepath.Join(vol1.Location(), "alpha"))
	c.Assert(os.IsNotExist(err), Equals, true)
	_, err = os.Stat(filepath.Join(vol2.Location(), "beta"))
	c.Assert(os.IsNotExist(err), Equals, true)

	// restore
	vman, err = volumemanager.New(
		vmanDBfilePath,
		func() (volume.Provider, error) {
			c.Fatal("default provider setup should not be called if the previous provider was restored")
			return nil, nil
		},
	)
	c.Assert(err, IsNil)

	// assert volumes
	restoredVolumes := vman.Volumes()
	c.Assert(restoredVolumes, HasLen, 2)
	c.Assert(restoredVolumes[vol1.Info().ID], NotNil)
	c.Assert(restoredVolumes[vol2.Info().ID], NotNil)

	// switch to the new volume references; do a bunch of smell checks on those
	vol1restored := restoredVolumes[vol1.Info().ID]
	vol2restored := restoredVolumes[vol2.Info().ID]
	c.Assert(vol1restored.Info(), DeepEquals, vol1.Info())
	c.Assert(vol2restored.Info(), DeepEquals, vol2.Info())
	c.Assert(vol1restored.Provider(), NotNil)
	c.Assert(vol1restored.Provider(), Equals, vol2restored.Provider())

	// assert named volumes
	namedVolumes := vman.NamedVolumes()
	c.Assert(namedVolumes, HasLen, 1)
	c.Assert(namedVolumes["aname"], Equals, vol2.Info().ID)

	// assert existences of filesystems and previous data
	c.Assert(vol1restored.Location(), testutils.DirContains, []string{"alpha"})
	c.Assert(vol2restored.Location(), testutils.DirContains, []string{"beta"})
}

// covers deletion persistence for a (named) volume
func (s *PersistenceTests) TestVolumeDeletion(c *C) {
	idString := random.String(12)
	vmanDBfilePath := fmt.Sprintf("/tmp/flynn-volumes-%s.bolt", idString)
	zfsDatasetName := fmt.Sprintf("flynn-test-dataset-%s", idString)
	zfsVdevFilePath := fmt.Sprintf("/tmp/flynn-test-zpool-%s.vdev", idString)
	defer os.Remove(vmanDBfilePath)
	defer os.Remove(zfsVdevFilePath)
	defer func() {
		pool, _ := gzfs.GetZpool(zfsDatasetName)
		if pool != nil {
			if datasets, err := pool.Datasets(); err == nil {
				for _, dataset := range datasets {
					dataset.Destroy(gzfs.DestroyRecursive | gzfs.DestroyForceUmount)
					os.Remove(dataset.Mountpoint)
				}
			}
			err := pool.Destroy()
			c.Assert(err, IsNil)
		}
	}()

	// new volume provider with a new backing zfs vdev file
	volProv, err := zfs.NewProvider(&zfs.ProviderConfig{
		DatasetName: zfsDatasetName,
		Make: &zfs.MakeDev{
			BackingFilename: zfsVdevFilePath,
			Size:            int64(math.Pow(2, float64(30))),
		},
	})
	c.Assert(err, IsNil)

	// new volume manager with that shiny new backing zfs vdev file and a new boltdb
	vman, err := volumemanager.New(
		vmanDBfilePath,
		func() (volume.Provider, error) { return volProv, nil },
	)
	c.Assert(err, IsNil)

	// make a named volume
	vol, err := vman.CreateOrGetNamedVolume("aname", "")
	c.Assert(err, IsNil)

	// assert existence of filesystems; emplace some data
	f, err := os.Create(filepath.Join(vol.Location(), "alpha"))
	c.Assert(err, IsNil)
	f.Close()

	// delete the volume again
	err = vman.DestroyVolume(vol.Info().ID)
	c.Assert(err, IsNil)

	// close persistence
	c.Assert(vman.PersistenceDBClose(), IsNil)

	// hack zfs export/umounting to emulate host shutdown
	err = exec.Command("zpool", "export", "-f", zfsDatasetName).Run()
	c.Assert(err, IsNil)

	// sanity check: assert the filesystems are gone
	// note that the directories remain present after 'zpool export'
	_, err = os.Stat(filepath.Join(vol.Location(), "alpha"))
	c.Assert(os.IsNotExist(err), Equals, true)

	// restore
	vman, err = volumemanager.New(
		vmanDBfilePath,
		func() (volume.Provider, error) {
			c.Fatal("default provider setup should not be called if the previous provider was restored")
			return nil, nil
		},
	)
	c.Assert(err, IsNil)

	// assert volumes gone
	restoredVolumes := vman.Volumes()
	c.Assert(restoredVolumes, HasLen, 0)

	// assert named volumes gone
	namedVolumes := vman.NamedVolumes()
	c.Assert(namedVolumes, HasLen, 0)

	// assert volume mount locations are gone
	_, err = os.Stat(vol.Location())
	c.Assert(os.IsNotExist(err), Equals, true)
}
