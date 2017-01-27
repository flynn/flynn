package volumemanager_test

import (
	"bytes"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/flynn/flynn/host/volume"
	"github.com/flynn/flynn/host/volume/manager"
	"github.com/flynn/flynn/host/volume/zfs"
	"github.com/flynn/flynn/pkg/random"
	"github.com/flynn/flynn/pkg/testutils"
	. "github.com/flynn/go-check"
	gzfs "github.com/mistifyio/go-zfs"
	"gopkg.in/inconshreveable/log15.v2"
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

func assertInfoEqual(c *C, volA, volB volume.Volume) {
	c.Assert(volA.Info().ID, Equals, volB.Info().ID)
	c.Assert(volA.Info().Type, Equals, volB.Info().Type)
	c.Assert(volA.Info().Meta, DeepEquals, volB.Info().Meta)
	c.Assert(volA.Info().CreatedAt.Equal(volB.Info().CreatedAt), Equals, true)
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
	vman := volumemanager.New(
		vmanDBfilePath,
		log15.New(),
		func() (volume.Provider, error) { return volProv, nil },
	)
	c.Assert(vman.OpenDB(), IsNil)

	// make two volumes
	vol1, err := vman.NewVolume(nil)
	c.Assert(err, IsNil)
	vol2, err := vman.NewVolume(nil)
	c.Assert(err, IsNil)

	// assert existence of filesystems; emplace some data
	f, err := os.Create(filepath.Join(vol1.Location(), "alpha"))
	c.Assert(err, IsNil)
	f.Close()

	// close persistence
	c.Assert(vman.CloseDB(), IsNil)

	// delete the second volume so we can check it doesn't prevent
	// a later restore
	c.Assert(volProv.DestroyVolume(vol2), IsNil)

	// hack zfs export/umounting to emulate host shutdown
	err = exec.Command("zpool", "export", "-f", zfsDatasetName).Run()
	c.Assert(err, IsNil)

	// sanity check: assert the filesystems are gone
	// note that the directories remain present after 'zpool export'
	_, err = os.Stat(filepath.Join(vol1.Location(), "alpha"))
	c.Assert(os.IsNotExist(err), Equals, true)

	// restore
	vman = volumemanager.New(
		vmanDBfilePath,
		log15.New(),
		func() (volume.Provider, error) {
			c.Fatal("default provider setup should not be called if the previous provider was restored")
			return nil, nil
		},
	)
	c.Assert(vman.OpenDB(), IsNil)

	// assert volumes
	restoredVolumes := vman.Volumes()
	c.Assert(restoredVolumes, HasLen, 1)
	c.Assert(restoredVolumes[vol1.Info().ID], NotNil)
	c.Assert(restoredVolumes[vol2.Info().ID], IsNil)

	// switch to the new volume references; do a bunch of smell checks on those
	vol1restored := restoredVolumes[vol1.Info().ID]
	assertInfoEqual(c, vol1restored, vol1)
	c.Assert(vol1restored.Provider(), NotNil)

	// assert existences of filesystems and previous data
	c.Assert(vol1restored.Location(), testutils.DirContains, []string{"alpha"})
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
	vman := volumemanager.New(
		vmanDBfilePath,
		log15.New(),
		func() (volume.Provider, error) { return volProv, nil },
	)
	c.Assert(vman.OpenDB(), IsNil)

	// make a named volume
	vol, err := vman.NewVolume(nil)
	c.Assert(err, IsNil)

	// assert existence of filesystems; emplace some data
	f, err := os.Create(filepath.Join(vol.Location(), "alpha"))
	c.Assert(err, IsNil)
	f.Close()

	// delete the volume again
	err = vman.DestroyVolume(vol.Info().ID)
	c.Assert(err, IsNil)

	// close persistence
	c.Assert(vman.CloseDB(), IsNil)

	// hack zfs export/umounting to emulate host shutdown
	err = exec.Command("zpool", "export", "-f", zfsDatasetName).Run()
	c.Assert(err, IsNil)

	// sanity check: assert the filesystems are gone
	// note that the directories remain present after 'zpool export'
	_, err = os.Stat(filepath.Join(vol.Location(), "alpha"))
	c.Assert(os.IsNotExist(err), Equals, true)

	// restore
	vman = volumemanager.New(
		vmanDBfilePath,
		log15.New(),
		func() (volume.Provider, error) {
			c.Fatal("default provider setup should not be called if the previous provider was restored")
			return nil, nil
		},
	)
	c.Assert(vman.OpenDB(), IsNil)

	// assert volumes gone
	restoredVolumes := vman.Volumes()
	c.Assert(restoredVolumes, HasLen, 0)

	// assert volume mount locations are gone
	_, err = os.Stat(vol.Location())
	c.Assert(os.IsNotExist(err), Equals, true)
}

func (s *PersistenceTests) TestSnapshotPersistence(c *C) {
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
	vman := volumemanager.New(
		vmanDBfilePath,
		log15.New(),
		func() (volume.Provider, error) { return volProv, nil },
	)
	c.Assert(vman.OpenDB(), IsNil)

	// make a volume
	vol1, err := vman.NewVolume(nil)
	c.Assert(err, IsNil)

	// assert existence of filesystems; emplace some data
	f, err := os.Create(filepath.Join(vol1.Location(), "alpha"))
	c.Assert(err, IsNil)
	f.Close()

	// snapshot it
	snap, err := vman.CreateSnapshot(vol1.Info().ID)

	// close persistence
	c.Assert(vman.CloseDB(), IsNil)

	// hack zfs export/umounting to emulate host shutdown
	err = exec.Command("zpool", "export", "-f", zfsDatasetName).Run()
	c.Assert(err, IsNil)

	// sanity check: assert the filesystems are gone
	// note that the directories remain present after 'zpool export'
	_, err = os.Stat(filepath.Join(vol1.Location(), "alpha"))
	c.Assert(os.IsNotExist(err), Equals, true)
	_, err = os.Stat(filepath.Join(snap.Location(), "alpha"))
	c.Assert(os.IsNotExist(err), Equals, true)

	// restore
	vman = volumemanager.New(
		vmanDBfilePath,
		log15.New(),
		func() (volume.Provider, error) {
			c.Fatal("default provider setup should not be called if the previous provider was restored")
			return nil, nil
		},
	)
	c.Assert(vman.OpenDB(), IsNil)

	// assert volumes
	restoredVolumes := vman.Volumes()
	c.Assert(restoredVolumes, HasLen, 2)
	c.Assert(restoredVolumes[vol1.Info().ID], NotNil)
	c.Assert(restoredVolumes[snap.Info().ID], NotNil)

	// switch to the new volume references; do a bunch of smell checks on those
	vol1restored := restoredVolumes[vol1.Info().ID]
	snapRestored := restoredVolumes[snap.Info().ID]
	assertInfoEqual(c, vol1restored, vol1)
	assertInfoEqual(c, snapRestored, snap)
	c.Assert(vol1restored.Provider(), NotNil)
	c.Assert(vol1restored.Provider(), Equals, snapRestored.Provider())

	// assert existences of filesystems and previous data
	c.Assert(vol1restored.Location(), testutils.DirContains, []string{"alpha"})
	c.Assert(snapRestored.Location(), testutils.DirContains, []string{"alpha"})
}

func (s *PersistenceTests) TestTransmittedSnapshotPersistence(c *C) {
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
	vman := volumemanager.New(
		vmanDBfilePath,
		log15.New(),
		func() (volume.Provider, error) { return volProv, nil },
	)
	c.Assert(vman.OpenDB(), IsNil)

	// make a volume
	vol1, err := vman.NewVolume(nil)
	c.Assert(err, IsNil)

	// assert existence of filesystems; emplace some data
	f, err := os.Create(filepath.Join(vol1.Location(), "alpha"))
	c.Assert(err, IsNil)
	f.Close()

	// make a snapshot, make a new volume to receive it, and do the transmit
	snap, err := vman.CreateSnapshot(vol1.Info().ID)
	vol2, err := vman.NewVolume(nil)
	c.Assert(err, IsNil)
	var buf bytes.Buffer
	haves, err := vman.ListHaves(vol2.Info().ID)
	c.Assert(err, IsNil)
	err = vman.SendSnapshot(snap.Info().ID, haves, &buf)
	c.Assert(err, IsNil)
	snapTransmitted, err := vman.ReceiveSnapshot(vol2.Info().ID, &buf)

	// sanity check: snapshot transmission worked
	c.Assert(vol2.Location(), testutils.DirContains, []string{"alpha"})
	c.Assert(snapTransmitted.Location(), testutils.DirContains, []string{"alpha"})

	// close persistence
	c.Assert(vman.CloseDB(), IsNil)

	// hack zfs export/umounting to emulate host shutdown
	err = exec.Command("zpool", "export", "-f", zfsDatasetName).Run()
	c.Assert(err, IsNil)

	// sanity check: assert the filesystems are gone
	// note that the directories remain present after 'zpool export'
	_, err = os.Stat(filepath.Join(snap.Location(), "alpha"))
	c.Assert(os.IsNotExist(err), Equals, true)
	_, err = os.Stat(filepath.Join(snapTransmitted.Location(), "alpha"))
	c.Assert(os.IsNotExist(err), Equals, true)

	// restore
	vman = volumemanager.New(
		vmanDBfilePath,
		log15.New(),
		func() (volume.Provider, error) {
			c.Fatal("default provider setup should not be called if the previous provider was restored")
			return nil, nil
		},
	)
	c.Assert(vman.OpenDB(), IsNil)

	// assert volumes
	restoredVolumes := vman.Volumes()
	c.Assert(restoredVolumes, HasLen, 4)
	c.Assert(restoredVolumes[vol1.Info().ID], NotNil)
	c.Assert(restoredVolumes[snap.Info().ID], NotNil)
	c.Assert(restoredVolumes[vol2.Info().ID], NotNil)
	c.Assert(restoredVolumes[snapTransmitted.Info().ID], NotNil)

	// still look like a snapshot?
	snapRestored := restoredVolumes[snapTransmitted.Info().ID]
	assertInfoEqual(c, snapRestored, snapTransmitted)
	c.Assert(snapRestored.IsSnapshot(), Equals, true)

	// assert existences of filesystems and previous data
	c.Assert(snapRestored.Location(), testutils.DirContains, []string{"alpha"})
}
