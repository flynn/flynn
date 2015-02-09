package zfs

import (
	"encoding/json"
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
	dataset   *zfs.Dataset
	basemount string
}

type Provider struct {
	config  *ProviderConfig
	dataset *zfs.Dataset
	volumes map[string]*zfsVolume
}

/*
	Describes zfs config used at provider setup time.

	`volume.ProviderSpec.Config` is deserialized to this for zfs.

	Also is the output of `MarshalGlobalState`.
*/
type ProviderConfig struct {
	// DatasetName specifies the zfs dataset this provider will create volumes under.
	//
	// If it doesn't specify an existing dataset, and `MakeDev` parameters have
	// been provided, those will be followed to create a zpool; otherwise
	// provider creation will fail.
	DatasetName string `json:"dataset"`

	Make *MakeDev `json:"makedev,omitempty"`

	// WorkingDir specifies the working directory zfs will use to expose mounts.
	// A default will be chosen if left blank.
	WorkingDir string `json:"working_dir"`
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
				// note: 'zpool import' recreated *all* the volume datasets in that pool.
				// currently, even if they're not known to a volume manager, they're not garbage collected.
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
	if config.WorkingDir == "" {
		config.WorkingDir = "/var/lib/flynn/volumes/zfs/"
	}
	return &Provider{
		config:  config,
		dataset: dataset,
		volumes: make(map[string]*zfsVolume),
	}, nil
}

func (b Provider) Kind() string {
	return "zfs"
}

func (b *Provider) NewVolume() (volume.Volume, error) {
	id := random.UUID()
	v := &zfsVolume{
		info:      &volume.Info{ID: id},
		provider:  b,
		basemount: filepath.Join(b.config.WorkingDir, "/mnt/", id),
	}
	var err error
	v.dataset, err = zfs.CreateFilesystem(path.Join(v.provider.dataset.Name, id), map[string]string{
		"mountpoint": v.basemount,
	})
	if err != nil {
		return nil, err
	}
	b.volumes[id] = v
	return v, nil
}

func (v *zfsVolume) Provider() volume.Provider {
	return v.provider
}

func (v *zfsVolume) Location() string {
	return v.basemount
}

func (b *Provider) MarshalGlobalState() (json.RawMessage, error) {
	return json.Marshal(b.config)
}

type zfsVolumeRecord struct {
	Dataset   string `json:"dataset"`
	Basemount string `json:"basemount"`
}

func (b *Provider) MarshalVolumeState(volumeID string) (json.RawMessage, error) {
	vol := b.volumes[volumeID]
	record := zfsVolumeRecord{}
	record.Dataset = vol.dataset.Name
	record.Basemount = vol.basemount
	return json.Marshal(record)
}

func (b *Provider) RestoreVolumeState(volInfo *volume.Info, data json.RawMessage) (volume.Volume, error) {
	record := &zfsVolumeRecord{}
	if err := json.Unmarshal(data, record); err != nil {
		return nil, fmt.Errorf("cannot restore volume %q: %s", volInfo.ID, err)
	}
	dataset, err := zfs.GetDataset(record.Dataset)
	if err != nil {
		return nil, fmt.Errorf("cannot restore volume %q: %s", volInfo.ID, err)
	}
	v := &zfsVolume{
		info:      volInfo,
		provider:  b,
		dataset:   dataset,
		basemount: record.Basemount,
	}
	b.volumes[volInfo.ID] = v
	return v, nil
}

func (v *zfsVolume) Info() *volume.Info {
	return v.info
}

func (v1 *zfsVolume) TakeSnapshot() (volume.Volume, error) {
	id := random.UUID()
	v2 := &zfsVolume{
		info:      &volume.Info{ID: id},
		provider:  v1.provider,
		basemount: filepath.Join(v1.provider.config.WorkingDir, "/mnt/", id),
	}
	var err error
	v2.dataset, err = cloneFilesystem(path.Join(v2.provider.dataset.Name, v2.info.ID), path.Join(v1.provider.dataset.Name, v1.info.ID), v2.basemount)
	if err != nil {
		return nil, err
	}
	v2.provider.volumes[id] = v2
	return v2, nil
}

func cloneFilesystem(newDatasetName string, parentDatasetName string, mountPath string) (*zfs.Dataset, error) {
	parentDataset, err := zfs.GetDataset(parentDatasetName)
	if parentDataset == nil {
		return nil, err
	}
	snapshotName := fmt.Sprintf("%d", time.Now().Nanosecond())
	snapshot, err := parentDataset.Snapshot(snapshotName, false)
	if err != nil {
		return nil, err
	}

	dataset, err := snapshot.Clone(newDatasetName, map[string]string{
		"mountpoint": mountPath,
	})
	if err != nil {
		snapshot.Destroy(zfs.DestroyDeferDeletion)
		return nil, err
	}
	err = snapshot.Destroy(zfs.DestroyDeferDeletion)
	return dataset, err
}
