package zfs

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/flynn/flynn/host/volume"
	"github.com/flynn/flynn/pkg/attempt"
	"github.com/flynn/flynn/pkg/random"
	zfs "github.com/mistifyio/go-zfs"
	"github.com/rancher/sparse-tools/sparse"
	"gopkg.in/inconshreveable/log15.v2"
)

// blockSize is the block size used when creating new zvols
const blockSize = 8 * 1024

const DefaultDatasetName = "flynn-default"

type zfsVolume struct {
	info       *volume.Info
	provider   *Provider
	dataset    *zfs.Dataset
	basemount  string
	filesystem *volume.Filesystem
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

func DefaultMakeDev(volPath string, log log15.Logger) *MakeDev {
	// use a zpool backing file size of either 70% of the device on which
	// volumes will reside, or 100GB if that can't be determined.
	log.Info("determining ZFS zpool size")
	var size int64
	var dev syscall.Statfs_t
	if err := syscall.Statfs(volPath, &dev); err == nil {
		size = (dev.Bsize * int64(dev.Blocks) * 7) / 10
	} else {
		size = 100000000000
	}
	log.Info(fmt.Sprintf("using ZFS zpool size %d", size))
	return &MakeDev{
		BackingFilename: filepath.Join(volPath, "zfs/vdev/flynn-default-zpool.vdev"),
		Size:            size,
	}
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
	for _, typ := range volume.VolumeTypes {
		if err := os.MkdirAll(filepath.Join(config.WorkingDir, "mnt", string(typ)), 0755); err != nil {
			return nil, err
		}
		dataset := filepath.Join(config.DatasetName, string(typ))
		if _, err := zfs.GetDataset(dataset); err != nil {
			if _, err := zfs.CreateFilesystem(dataset, nil); err != nil {
				return nil, err
			}
		}
	}
	return &Provider{
		config:  config,
		dataset: dataset,
		volumes: make(map[string]*zfsVolume),
	}, nil
}

func (p *Provider) Kind() string {
	return "zfs"
}

func (p *Provider) NewVolume(info *volume.Info) (volume.Volume, error) {
	if info == nil {
		info = &volume.Info{}
	}
	if info.ID == "" {
		info.ID = random.UUID()
	}
	info.Type = volume.VolumeTypeData
	info.CreatedAt = time.Now()
	v := &zfsVolume{
		info:      info,
		provider:  p,
		basemount: p.mountPath(info),
	}
	var err error
	v.dataset, err = zfs.CreateFilesystem(p.datasetPath(info), map[string]string{
		"mountpoint": v.basemount,
	})
	if err != nil {
		return nil, err
	}
	p.volumes[info.ID] = v
	return v, nil
}

var zvolOpenAttempts = attempt.Strategy{
	Total: 10 * time.Second,
	Delay: 10 * time.Millisecond,
}

func (p *Provider) ImportFilesystem(fs *volume.Filesystem) (volume.Volume, error) {
	if fs.ID == "" {
		fs.ID = random.UUID()
	}
	info := fs.Info()
	info.CreatedAt = time.Now()
	v := &zfsVolume{
		info:       info,
		provider:   p,
		basemount:  p.mountPath(info),
		filesystem: fs,
	}

	// align size to blockSize
	size := (fs.Size/blockSize + 1) * blockSize

	opts := map[string]string{
		"volblocksize": strconv.Itoa(blockSize),
	}
	if _, ok := fs.Data.(sparse.FileIoProcessor); ok {
		opts["refreservation"] = "none"
	}

	var err error
	v.dataset, err = zfs.CreateVolume(p.datasetPath(info), uint64(size), opts)
	if err != nil {
		return nil, err
	}

	// open the zvol device, trying multiple times as the device node is
	// created asynchronously
	var dev *os.File
	err = zvolOpenAttempts.Run(func() (err error) {
		dev, err = os.OpenFile(p.zvolPath(info), os.O_WRONLY, 0666)
		return
	})
	if err != nil {
		p.destroy(v)
		return nil, err
	}
	defer dev.Close()

	if f, ok := fs.Data.(sparse.FileIoProcessor); ok {
		if err := p.copySparse(dev, f); err != nil {
			p.destroy(v)
			return nil, err
		}
	} else {
		n, err := io.Copy(dev, fs.Data)
		if err != nil {
			p.destroy(v)
			return nil, err
		} else if n != fs.Size {
			p.destroy(v)
			return nil, io.ErrShortWrite
		}
	}

	if err = os.MkdirAll(v.basemount, 0755); err != nil {
		p.destroy(v)
		return nil, err
	}

	if err := syscall.Mount(dev.Name(), v.basemount, string(fs.Type), fs.MountFlags, ""); err != nil {
		p.destroy(v)
		return nil, err
	}

	p.volumes[fs.ID] = v
	return v, nil
}

func (p *Provider) copySparse(dst io.WriteSeeker, src sparse.FileIoProcessor) error {
	extents, err := sparse.GetFiemapExtents(src)
	if err != nil {
		return err
	}

	for _, x := range extents {
		if _, err := dst.Seek(int64(x.Logical), os.SEEK_SET); err != nil {
			return err
		}
		if _, err := io.Copy(dst, io.NewSectionReader(src, int64(x.Logical), int64(x.Length))); err != nil {
			return err
		}
	}

	return nil
}

func (p *Provider) owns(vol volume.Volume) (*zfsVolume, error) {
	zvol := p.volumes[vol.Info().ID]
	if zvol == nil {
		return nil, fmt.Errorf("volume does not belong to this provider")
	}
	if zvol != vol { // these pointers should be canonical
		panic(fmt.Errorf("volume does not belong to this provider"))
	}
	return zvol, nil
}

func (p *Provider) mountPath(info *volume.Info) string {
	return filepath.Join(p.config.WorkingDir, "mnt", string(info.Type), info.ID)
}

func (p *Provider) datasetPath(info *volume.Info) string {
	return filepath.Join(p.dataset.Name, string(info.Type), info.ID)
}

func (p *Provider) zvolPath(info *volume.Info) string {
	return filepath.Join("/dev/zvol", p.datasetPath(info))
}

func (p *Provider) DestroyVolume(v volume.Volume) error {
	vol, err := p.owns(v)
	if err != nil {
		return err
	}
	return p.destroy(vol)
}

func (p *Provider) destroy(vol *zfsVolume) error {
	if vol.IsSnapshot() || vol.filesystem != nil {
		if err := syscall.Unmount(vol.basemount, 0); err != nil {
			return err
		}
		os.Remove(vol.basemount)
	}
	if err := vol.dataset.Destroy(zfs.DestroyForceUmount); err != nil {
		for i := 0; i < 5 && err != nil && IsDatasetBusyError(err); i++ {
			// sometimes zfs will claim to be busy as if files are still open even when all container processes are dead.
			// usually this goes away, so retry a few times.
			time.Sleep(1 * time.Second)
			err = vol.dataset.Destroy(zfs.DestroyForceUmount)
		}
		if err != nil {
			return err
		}
	}
	if vol.basemount != "" {
		os.Remove(vol.basemount)
	}
	delete(p.volumes, vol.info.ID)
	return nil
}

func (p *Provider) CreateSnapshot(vol volume.Volume) (volume.Volume, error) {
	zvol, err := p.owns(vol)
	if err != nil {
		return nil, err
	}
	id := random.UUID()
	info := &volume.Info{ID: id, Type: vol.Info().Type}
	snap := &zfsVolume{
		info:      info,
		provider:  zvol.provider,
		basemount: p.mountPath(info),
	}
	snap.dataset, err = zvol.dataset.Snapshot(id, false)
	if err != nil {
		return nil, err
	}
	if err := p.mountDataset(snap); err != nil {
		return nil, err
	}
	p.volumes[id] = snap
	return snap, nil
}

func isMount(path string) (bool, error) {
	pathStat, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	parentStat, err := os.Stat(filepath.Dir(path))
	if err != nil {
		return false, err
	}
	pathDev := pathStat.Sys().(*syscall.Stat_t).Dev
	parentDev := parentStat.Sys().(*syscall.Stat_t).Dev
	return pathDev != parentDev, nil
}

func (p *Provider) mountDataset(vol *zfsVolume) error {
	// mount the dataset, snapshots will be readonly
	// 'zfs mount' currently can't perform on snapshots; seealso https://github.com/zfsonlinux/zfs/issues/173
	alreadyMounted, err := isMount(vol.basemount)
	if err != nil {
		return fmt.Errorf("could not mount: %s", err)
	}
	if alreadyMounted {
		return nil
	}
	if err = os.MkdirAll(vol.basemount, 0644); err != nil {
		return fmt.Errorf("could not mount: %s", err)
	}
	if vol.filesystem != nil {
		return syscall.Mount(
			p.zvolPath(vol.info),
			vol.basemount,
			string(vol.filesystem.Type),
			vol.filesystem.MountFlags,
			"",
		)
	}
	var buf bytes.Buffer
	var cmd *exec.Cmd
	if vol.IsSnapshot() {
		cmd = exec.Command("mount", "-tzfs", vol.dataset.Name, vol.basemount)
	} else {
		cmd = exec.Command("zfs", "mount", vol.dataset.Name)
	}
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("could not mount: %s (%s)", err, strings.TrimSpace(buf.String()))
	}
	return nil
}

func (p *Provider) ForkVolume(vol volume.Volume) (volume.Volume, error) {
	zvol, err := p.owns(vol)
	if err != nil {
		return nil, err
	}
	if !vol.IsSnapshot() {
		return nil, fmt.Errorf("can only fork a snapshot")
	}
	id := random.UUID()
	info := &volume.Info{ID: id, Type: vol.Info().Type}
	v2 := &zfsVolume{
		info:      info,
		provider:  zvol.provider,
		basemount: p.mountPath(info),
	}
	cloneID := fmt.Sprintf("%s/%s", zvol.provider.dataset.Name, id)
	v2.dataset, err = zvol.dataset.Clone(cloneID, map[string]string{
		"mountpoint": v2.basemount,
	})
	if err != nil {
		return nil, fmt.Errorf("could not fork volume: %s", err)
	}
	p.volumes[id] = v2
	return v2, nil
}

type zfsHaves struct {
	SnapID string `json:"snap_id"`
}

/*
	Returns the set of snapshot UIDs available in this volume's backing dataset.
*/
func (p *Provider) ListHaves(vol volume.Volume) ([]json.RawMessage, error) {
	zvol, err := p.owns(vol)
	if err != nil {
		return nil, err
	}
	snapshots, err := zvol.dataset.Snapshots()
	if err != nil {
		return nil, err
	}
	res := make([]json.RawMessage, len(snapshots))
	for i, snapshot := range snapshots {
		have := &zfsHaves{SnapID: strings.SplitN(snapshot.Name, "@", 2)[1]}
		serial, err := json.Marshal(have)
		if err != nil {
			return nil, err
		}
		res[i] = serial
	}
	return res, nil
}

func (p *Provider) SendSnapshot(vol volume.Volume, haves []json.RawMessage, output io.Writer) error {
	zvol, err := p.owns(vol)
	if err != nil {
		return err
	}
	if !vol.IsSnapshot() {
		return fmt.Errorf("can only send a snapshot")
	}
	// zfs recv can only really accept snapshots that apply to the current tip
	var latestRemote string
	if haves != nil && len(haves) > 0 {
		have := &zfsHaves{}
		if err := json.Unmarshal(haves[len(haves)-1], have); err == nil {
			latestRemote = have.SnapID
		}
	}
	// look for intersection of existing snapshots on this volume; if so do incremental
	parentName := strings.SplitN(zvol.dataset.Name, "@", 2)[0]
	parentDataset, err := zfs.GetDataset(parentName)
	if err != nil {
		return err
	}
	snapshots, err := parentDataset.Snapshots()
	if err != nil {
		return err
	}
	// we can fly incremental iff the latest snap on the remote is available here
	useIncremental := false
	if latestRemote != "" {
		for _, snap := range snapshots {
			if strings.SplitN(snap.Name, "@", 2)[1] == latestRemote {
				useIncremental = true
				break
			}
		}
	}
	// at last, send:
	if useIncremental {
		sendCmd := exec.Command("zfs", "send", "-i", latestRemote, zvol.dataset.Name)
		sendCmd.Stdout = output
		return sendCmd.Run()
	}
	return zvol.dataset.SendSnapshot(output)
}

/*
	ReceiveSnapshot both accepts a snapshotted filesystem as a byte stream,
	and applies that state to the given `vol` (i.e., if this were git, it's like
	`git fetch && git pull` at the same time; regretably, it's pretty hard to get
	zfs to separate those operations).  If there are local working changes in
	the volume, they will be overwritten.

	In addition to the given volume being mutated on disk, a reference to the
	new snapshot will be returned (this can be used for cleanup, though be aware
	that with zfs, removing snapshots may impact the ability to use incremental
	deltas when receiving future snapshots).

	Also note that ZFS is *extremely* picky about receiving snapshots; in
	addition to obvious failure modes like an incremental snapshot with
	insufficient data, the following complications apply:
	- Sending an incremental snapshot with too much history will fail.
	- Sending a full snapshot to a volume with any other snapshots will fail.
	In the former case, you can renegociate; in the latter, you will have to
	either *destroy snapshots* or make a new volume.
*/
func (p *Provider) ReceiveSnapshot(vol volume.Volume, input io.Reader) (volume.Volume, error) {
	zvol, err := p.owns(vol)
	if err != nil {
		return nil, err
	}
	// recv does the right thing with input either fresh or incremental.
	// recv with the dataset name and no snapshot suffix means the snapshot name from farside is kept;
	// this is important because though we've assigned it a new UUID, the zfs dataset name match is used for incr hinting.
	var buf bytes.Buffer
	recvCmd := exec.Command("zfs", "recv", "-F", zvol.dataset.Name)
	recvCmd.Stdin = input
	recvCmd.Stderr = &buf
	if err := recvCmd.Run(); err != nil {
		return nil, fmt.Errorf("zfs recv rejected snapshot data: %s (%s)", err, strings.TrimSpace(buf.String()))
	}
	// get the dataset reference back; whatever the latest snapshot is must be what we received
	snapshots, err := zvol.dataset.Snapshots()
	if err != nil {
		return nil, err
	}
	if len(snapshots) == 0 {
		// should never happen, unless someone else is racing the zfs controls
		return nil, fmt.Errorf("zfs recv misplaced snapshot data")
	}
	snapds := snapshots[len(snapshots)-1]
	// reassemble as a flynn volume for return
	id := random.UUID()
	info := &volume.Info{ID: id, Type: vol.Info().Type}
	snap := &zfsVolume{
		info:      info,
		provider:  zvol.provider,
		dataset:   snapds,
		basemount: p.mountPath(info),
	}
	if err := p.mountDataset(snap); err != nil {
		return nil, err
	}
	p.volumes[id] = snap
	return snap, nil
}

func (v *zfsVolume) Provider() volume.Provider {
	return v.provider
}

func (v *zfsVolume) Location() string {
	return v.basemount
}

func (p *Provider) MarshalGlobalState() (json.RawMessage, error) {
	return json.Marshal(p.config)
}

type zfsVolumeRecord struct {
	Dataset    string             `json:"dataset"`
	Basemount  string             `json:"basemount"`
	Filesystem *volume.Filesystem `json:"filesystem,omitempty"`
}

func (p *Provider) MarshalVolumeState(volumeID string) (json.RawMessage, error) {
	vol := p.volumes[volumeID]
	record := zfsVolumeRecord{
		Dataset:    vol.dataset.Name,
		Basemount:  vol.basemount,
		Filesystem: vol.filesystem,
	}
	return json.Marshal(record)
}

func (p *Provider) RestoreVolumeState(volInfo *volume.Info, data json.RawMessage) (volume.Volume, error) {
	record := &zfsVolumeRecord{}
	if err := json.Unmarshal(data, record); err != nil {
		return nil, fmt.Errorf("cannot restore volume %q: %s", volInfo.ID, err)
	}
	// handle legacy info which don't have a Type
	if volInfo.Type == "" {
		if record.Filesystem != nil {
			volInfo.Type = record.Filesystem.Type
		} else {
			volInfo.Type = volume.VolumeTypeData
		}
	}
	dataset, err := zfs.GetDataset(record.Dataset)
	if err != nil {
		if isDatasetNotExistsError(err) {
			return nil, volume.ErrNoSuchVolume
		}
		return nil, fmt.Errorf("cannot restore volume %q: %s", volInfo.ID, err)
	}
	v := &zfsVolume{
		info:       volInfo,
		provider:   p,
		dataset:    dataset,
		basemount:  record.Basemount,
		filesystem: record.Filesystem,
	}
	if err := p.mountDataset(v); err != nil {
		return nil, err
	}
	p.volumes[volInfo.ID] = v
	return v, nil
}

func (v *zfsVolume) Info() *volume.Info {
	return v.info
}

func (v *zfsVolume) IsSnapshot() bool {
	return v.dataset.Type == zfs.DatasetSnapshot
}
