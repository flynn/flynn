package zfs

import (
	"io/ioutil"
	"math"
	"os"

	zfs "github.com/flynn/flynn/Godeps/_workspace/src/github.com/mistifyio/go-zfs"
)

func WithTmpfileZpool(poolName string, fn func() error) error {
	backingFile, err := ioutil.TempFile("/tmp/", "zfs-")
	if err != nil {
		return err
	}
	defer backingFile.Close()

	err = backingFile.Truncate(int64(math.Pow(2, float64(30))))
	if err != nil {
		return err
	}
	defer os.Remove(backingFile.Name())
	pool, err := zfs.CreateZpool(
		poolName,
		nil,
		"-mnone", // do not mount the root dataset.  (we'll mount our own datasets as necessary.)
		backingFile.Name(),
	)
	if err != nil {
		return err
	}
	defer func() {
		if err := pool.Destroy(); err != nil {
			panic(err)
		}
	}()

	return fn()
}
