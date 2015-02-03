package zfs

import (
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"syscall"
	"time"

	zfs "github.com/flynn/flynn/Godeps/_workspace/src/github.com/mistifyio/go-zfs"
	"github.com/flynn/flynn/host/volume"
	"github.com/flynn/flynn/pkg/random"
)

type zfsVolume struct {
	info      *volume.Info
	provider  *Provider
	mounts    map[volume.VolumeMount]struct{}
	basemount string // This is the location of the main mount of the ZFS dataset.  Mounts into containers are bind-mounts pointing back out to this.  The user does not control it (it is essentially an implementation detail).
}

type Provider struct {
	config  *ProviderConfig
	dataset *zfs.Dataset
}

/*
	Describes zfs config used at provider setup time.

	`volume.ProviderSpec.Config` is deserialized to this for zfs.
*/
type ProviderConfig struct {
	// DatasetName specifies the zfs dataset this provider will create volumes under.
	//
	// If it doesn't specify an existing dataset, and `MakeDev` parameters have
	// been provided, those will be followed to create a zpool; otherwise
	// provider creation will fail.
	DatasetName string `json:"dataset"`

	Make *MakeDev `json:"makedev,omitempty"`
}

/*
	Describes parameters for creating a zpool.

	Currently this only supports file-type vdevs; be aware that these are
	convenient, but may have limited performance.  Advanced users should
	consider configuring a zpool using block devices directly, and specifing
	use of datasets in those zpools those rather than this fallback mechanism.
*/
type MakeDev struct {
	BackingFilename string `json:"filename"`
	Size            int64  `json:"size"`
}

func NewProvider(config *ProviderConfig) (volume.Provider, error) {
	if _, err := exec.LookPath("zfs"); err != nil {
		return nil, fmt.Errorf("zfs command is not available")
	}
	dataset, err := zfs.GetDataset(config.DatasetName)
	if err != nil {
		if isDatasetNotExistsError(err) {
			// if the dataset doesn't exist...
			if config.Make == nil {
				// not much we can do without a dataset or pool to contain data
				return nil, err
			}
			// make a zpool backed by a sparse file.  it's the most portable thing we can do.
			if err := os.MkdirAll(filepath.Dir(config.Make.BackingFilename), 0755); err != nil {
				return nil, err
			}
			f, err := os.OpenFile(config.Make.BackingFilename, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if err == nil {
				// if we've created a new file, size it and create a new zpool
				if err = f.Truncate(config.Make.Size); err != nil {
					return nil, err
				}
				f.Close()
				if _, err = zfs.CreateZpool(
					config.DatasetName,
					nil,
					"-mnone", // do not mount the root dataset.  (we'll mount our own datasets as necessary.)
					config.Make.BackingFilename,
				); err != nil {
					return nil, err
				}
			} else if err.(*os.PathError).Err == syscall.EEXIST {
				// if the file already exists, check it for existing zpool
				if err := zpoolImportFile(config.Make.BackingFilename); err != nil {
					// if 'zpool import' didn't believe it... halt here
					// we could overwrite but we'd rather stop and avoid potential data loss.
					return nil, fmt.Errorf("error attempting import of existing zpool file: %s", err)
				}
			} else {
				return nil, err
			}
			// get the dataset again... `zfs.Zpool` isn't a `zfs.Dataset`
			dataset, err = zfs.GetDataset(config.DatasetName)
			if err != nil {
				return nil, err
			}
		} else {
			// any error more complicated than not_exists, and we're out of our depth
			return nil, err
		}
	}
	return &Provider{
		config:  config,
		dataset: dataset,
	}, nil
}

func (b *Provider) NewVolume() (volume.Volume, error) {
	id := random.UUID()
	v := &zfsVolume{
		info:      &volume.Info{ID: id},
		provider:  b,
		mounts:    make(map[volume.VolumeMount]struct{}),
		basemount: filepath.Join("/var/lib/flynn/volumes/zfs/mnt/", id),
	}
	if _, err := zfs.CreateFilesystem(path.Join(v.provider.dataset.Name, id), map[string]string{
		"mountpoint": v.basemount,
	}); err != nil {
		return nil, err
	}
	return v, nil
}

func (v *zfsVolume) Provider() volume.Provider {
	return v.provider
}

func (v *zfsVolume) Info() *volume.Info {
	return v.info
}

func (v *zfsVolume) Mounts() map[volume.VolumeMount]struct{} {
	return v.mounts
}

func (v *zfsVolume) Mount(jobId, path string) (string, error) {
	mount := volume.VolumeMount{
		JobID:    jobId,
		Location: path,
	}
	if _, exists := v.mounts[mount]; exists {
		return "", fmt.Errorf("volume: cannot make same mount twice!")
	}
	v.mounts[mount] = struct{}{}
	return v.basemount, nil
}

func (v1 *zfsVolume) TakeSnapshot() (volume.Volume, error) {
	id := random.UUID()
	v2 := &zfsVolume{
		info:      &volume.Info{ID: id},
		provider:  v1.provider,
		mounts:    make(map[volume.VolumeMount]struct{}),
		basemount: filepath.Join("/var/lib/flynn/volumes/zfs/", id),
	}
	if err := cloneFilesystem(path.Join(v2.provider.dataset.Name, v2.info.ID), path.Join(v1.provider.dataset.Name, v1.info.ID), v2.basemount); err != nil {
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
