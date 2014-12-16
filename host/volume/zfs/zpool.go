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
	pool, err := zfs.CreateZpool(poolName, nil, backingFile.Name()) // the default point where this mounts is in "/poolName", so... you're gonna wanna override that
	if err != nil {
		return err
	}
	defer pool.Destroy()

	return fn()
}
