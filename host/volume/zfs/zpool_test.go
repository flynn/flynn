package zfs

import (
	"fmt"
	"io/ioutil"
	"math"
	"os"
	"os/exec"
	"path"

	"github.com/flynn/flynn/pkg/random"
	"github.com/flynn/flynn/pkg/testutils"
	. "github.com/flynn/go-check"
	gzfs "github.com/mistifyio/go-zfs"
)

// note: whimsical/unique dataset names per test are chosen to help debug
// in the event of stateful catastrophe (zfs is quite capable of carrying state
// between tests!).

type ZpoolTests struct{}

var _ = Suite(&ZpoolTests{})

func (ZpoolTests) SetUpSuite(c *C) {
	// Skip all tests in this suite if not running as root.
	// Many zfs operations require root priviledges.
	testutils.SkipIfNotRoot(c)
}

var one_gig = int64(math.Pow(2, float64(30)))

func (ZpoolTests) TestProviderRequestingNonexistentZpoolFails(c *C) {
	dataset := "testpool-starfish"
	provider, err := NewProvider(&ProviderConfig{
		DatasetName: dataset,
		// no spec for making something, so this should be an error
	})
	c.Assert(provider, IsNil)
	c.Assert(err, NotNil)
	c.Assert(isDatasetNotExistsError(err), Equals, true)
}

func (ZpoolTests) TestProviderAutomaticFileVdevZpoolCreation(c *C) {
	dataset := "testpool-dinosaur"

	// don't actually use ioutil.Tempfile;
	// we want to exerise the path where the file doesn't exist.
	backingFilePath := fmt.Sprintf("/tmp/zfs-%s", random.String(12))
	defer os.Remove(backingFilePath)

	provider, err := NewProvider(&ProviderConfig{
		DatasetName: dataset,
		Make: &MakeDev{
			BackingFilename: backingFilePath,
			Size:            one_gig,
		},
	})
	defer func() {
		pool, _ := gzfs.GetZpool(dataset)
		if pool != nil {
			pool.Destroy()
		}
	}()
	c.Assert(err, IsNil)
	c.Assert(provider, NotNil)

	// also, we shouldn't get any '/testpool' dir at root
	_, err = os.Stat(dataset)
	c.Assert(err, NotNil)
	c.Assert(os.IsNotExist(err), Equals, true)
}

func (ZpoolTests) TestProviderExistingZpoolDetection(c *C) {
	dataset := "testpool-festival"

	backingFilePath := fmt.Sprintf("/tmp/zfs-%s", random.String(12))
	defer os.Remove(backingFilePath)

	provider, err := NewProvider(&ProviderConfig{
		DatasetName: dataset,
		Make: &MakeDev{
			BackingFilename: backingFilePath,
			Size:            one_gig,
		},
	})
	defer func() {
		pool, _ := gzfs.GetZpool(dataset)
		if pool != nil {
			pool.Destroy()
		}
	}()
	c.Assert(err, IsNil)
	c.Assert(provider, NotNil)

	// if we create another provider with the same dataset, it should
	// see the existing one and thus shouldn't hit the MakeDev path
	badFilePath := "/tmp/zfs-test-should-not-exist"
	provider, err = NewProvider(&ProviderConfig{
		DatasetName: dataset,
		Make: &MakeDev{
			BackingFilename: badFilePath,
			Size:            one_gig,
		},
	})
	c.Assert(err, IsNil)
	c.Assert(provider, NotNil)
	_, err = os.Stat(badFilePath)
	c.Assert(err, NotNil)
	c.Assert(os.IsNotExist(err), Equals, true)
}

func (ZpoolTests) TestOrphanedZpoolFileAdoption(c *C) {
	dataset := "testpool-bananagram"

	backingFilePath := fmt.Sprintf("/tmp/zfs-%s", random.String(12))
	defer os.Remove(backingFilePath)

	provider, err := NewProvider(&ProviderConfig{
		DatasetName: dataset,
		Make: &MakeDev{
			BackingFilename: backingFilePath,
			Size:            one_gig,
		},
	})
	defer func() {
		pool, _ := gzfs.GetZpool(dataset)
		if pool != nil {
			pool.Destroy()
		}
	}()
	c.Assert(err, IsNil)
	c.Assert(provider, NotNil)

	// add a dataset to this zpool, so we can check for it on the flip side
	markerDatasetName := path.Join(dataset, "testfs")
	_, err = gzfs.CreateFilesystem(markerDatasetName, nil)
	c.Assert(err, IsNil)

	// do a 'zpool export'
	// this roughly approximates what we see after a host reboot
	// (though host reboots may leave it in an even more unclean state than this)
	err = exec.Command("zpool", "export", "-f", dataset).Run()
	c.Assert(err, IsNil)

	// sanity check that our test is doing the right thing: zpool forgot about these
	_, err = gzfs.GetDataset(dataset)
	c.Assert(err, NotNil)
	_, err = gzfs.GetDataset(markerDatasetName)
	c.Assert(err, NotNil)

	// if we create another provider with the same file vdev path, it should
	// pick up that file again without wrecking the dataset
	provider, err = NewProvider(&ProviderConfig{
		DatasetName: dataset,
		Make: &MakeDev{
			BackingFilename: backingFilePath,
			Size:            one_gig,
		},
	})
	c.Assert(err, IsNil)
	c.Assert(provider, NotNil)
	_, err = gzfs.GetDataset(markerDatasetName)
	c.Assert(err, IsNil)
}

func (ZpoolTests) TestNonZpoolFilesFailImport(c *C) {
	dataset := "testpool-landslide"

	backingFile, err := ioutil.TempFile("/tmp/", "zfs-")
	c.Assert(err, IsNil)
	backingFile.Write([]byte{'a', 'b', 'c'})
	backingFile.Close()

	provider, err := NewProvider(&ProviderConfig{
		DatasetName: dataset,
		Make: &MakeDev{
			BackingFilename: backingFile.Name(),
			Size:            one_gig,
		},
	})
	defer func() {
		pool, _ := gzfs.GetZpool(dataset)
		if pool != nil {
			pool.Destroy()
		}
	}()
	c.Assert(err, NotNil)
	c.Assert(provider, IsNil)
}
