package zfs

import (
	"fmt"
	"os/exec"
	"path"
	"path/filepath"
	"time"

	zfs "github.com/flynn/flynn/Godeps/_workspace/src/github.com/mistifyio/go-zfs"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/host/volume"
	"github.com/flynn/flynn/pkg/random"
)

type zfsVolume struct {
	id        string
	mounts    map[volume.VolumeMount]struct{}
	poolName  string // The name of the zpool this storage is cut from.  (We need this when forking snapshots, or doing some inspections.)
	basemount string // This is the location of the main mount of the ZFS dataset.  Mounts into containers are bind-mounts pointing back out to this.  The user does not control it (it is essentially an implementation detail).
}

type Provider struct {
	poolName string
}

func NewProvider(poolName string) (volume.Provider, error) {
	if _, err := exec.LookPath("zfs"); err != nil {
		return nil, fmt.Errorf("zfs command is not available")
	}
	return &Provider{
		poolName: poolName,
	}, nil
}

func (b Provider) NewVolume() (volume.Volume, error) {
	id := random.UUID()
	v := &zfsVolume{
		id:        id,
		mounts:    make(map[volume.VolumeMount]struct{}),
		poolName:  b.poolName,
		basemount: filepath.Join("/var/lib/flynn/volumes/zfs/", id),
	}
	if _, err := zfs.CreateFilesystem(path.Join(v.poolName, v.id), map[string]string{
		"mountpoint": v.basemount,
	}); err != nil {
		return nil, err
	}
	return v, nil
}

func (v *zfsVolume) ID() string {
	return v.id
}

func (v *zfsVolume) Mounts() map[volume.VolumeMount]struct{} {
	return v.mounts
}

func (v *zfsVolume) Mount(job host.ActiveJob, path string) (volume.VolumeMount, error) {
	mount := volume.VolumeMount{
		JobID:    job.Job.ID,
		Location: path,
	}
	if _, exists := v.mounts[mount]; exists {
		return volume.VolumeMount{}, fmt.Errorf("volume: cannot make same mount twice!")
	}
	// TODO: fire syscalls
	v.mounts[mount] = struct{}{}
	return mount, nil
}

func (v1 *zfsVolume) TakeSnapshot() (volume.Volume, error) {
	id := random.UUID()
	v2 := &zfsVolume{
		id:        id,
		mounts:    make(map[volume.VolumeMount]struct{}),
		poolName:  v1.poolName,
		basemount: filepath.Join("/var/lib/flynn/volumes/zfs/", id),
	}
	if err := cloneFilesystem(path.Join(v2.poolName, v2.id), path.Join(v1.poolName, v1.id), v2.basemount); err != nil {
		return nil, err
	}
	return v2, nil
}

func cloneFilesystem(newDatasetName string, parentDatasetName string, mountPath string) error {
	parentDataset, err := zfs.GetDataset(parentDatasetName)
	if parentDataset == nil {
		return err
	}
	snapshotName := fmt.Sprintf("%d", time.Now().Nanosecond())
	snapshot, err := parentDataset.Snapshot(snapshotName, false)
	if err != nil {
		return err
	}

	_, err = snapshot.Clone(newDatasetName, map[string]string{
		"mountpoint": mountPath,
	})
	if err != nil {
		snapshot.Destroy(zfs.DestroyDeferDeletion)
		return err
	}
	err = snapshot.Destroy(zfs.DestroyDeferDeletion)
	return err
}
