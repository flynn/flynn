package zfs

import (
	"fmt"
	"math"
	"os"
	"testing"

	"github.com/flynn/flynn/host/volume"
	"github.com/flynn/flynn/pkg/random"
	. "github.com/flynn/go-check"
	gzfs "github.com/mistifyio/go-zfs"
)

func Test(t *testing.T) { TestingT(t) }

/*
	Helper for temporary zpools, embeddable in tests.
*/
type TempZpool struct {
	IDstring          string
	ZpoolVdevFilePath string
	ZpoolName         string
	VolProv           volume.Provider
}

func (s *TempZpool) SetUpTest(c *C) {
	if s.IDstring == "" {
		s.IDstring = random.String(12)
	}

	// Set up a new provider with a zpool that will be destroyed on teardown
	s.ZpoolVdevFilePath = fmt.Sprintf("/tmp/flynn-test-zpool-%s.vdev", s.IDstring)
	s.ZpoolName = fmt.Sprintf("flynn-test-zpool-%s", s.IDstring)
	var err error
	s.VolProv, err = NewProvider(&ProviderConfig{
		DatasetName: s.ZpoolName,
		Make: &MakeDev{
			BackingFilename: s.ZpoolVdevFilePath,
			Size:            int64(math.Pow(2, float64(30))),
		},
	})
	c.Assert(err, IsNil)
}

func (s *TempZpool) TearDownTest(c *C) {
	if s.ZpoolVdevFilePath != "" {
		os.Remove(s.ZpoolVdevFilePath)
	}
	pool, _ := gzfs.GetZpool(s.ZpoolName)
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
}
