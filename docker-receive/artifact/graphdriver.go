package main

import (
	"io"
	"os"
	"path/filepath"

	"github.com/docker/docker/daemon/graphdriver"
	"github.com/docker/docker/pkg/archive"
	"github.com/docker/docker/pkg/idtools"
)

func init() {
	graphdriver.Register("flynn", newGraphDriver)
}

func newGraphDriver(root string, options []string, uidMaps, gidMaps []idtools.IDMap) (graphdriver.Driver, error) {
	if err := os.MkdirAll(root, 0755); err != nil {
		return nil, err
	}
	return &GraphDriver{root}, nil
}

// GraphDriver implements the graphdriver.Driver interface and just stores
// diffs as single files on the filesystem
type GraphDriver struct {
	root string
}

func (d *GraphDriver) String() string {
	return "flynn"
}

func (d *GraphDriver) Status() [][2]string {
	return nil
}

func (d *GraphDriver) GetMetadata(id string) (map[string]string, error) {
	return nil, nil
}

func (d *GraphDriver) Cleanup() error {
	return nil
}

func (d *GraphDriver) Create(id, parent string) error {
	return os.MkdirAll(d.dir(id), 0755)
}

func (d *GraphDriver) Remove(id string) error {
	return os.RemoveAll(d.dir(id))
}

func (d *GraphDriver) Get(id, mountLabel string) (string, error) {
	return d.dir(id), nil
}

func (d *GraphDriver) Put(id string) error {
	return nil
}

func (d *GraphDriver) Exists(id string) bool {
	_, err := os.Stat(d.dir(id))
	return err == nil
}

func (d *GraphDriver) Diff(id, parent string) (archive.Archive, error) {
	return os.Open(d.path(id))
}

func (d *GraphDriver) Changes(id, parent string) ([]archive.Change, error) {
	return nil, nil
}

func (d *GraphDriver) ApplyDiff(id, parent string, diff archive.Reader) (int64, error) {
	f, err := os.Create(d.path(id))
	if err != nil {
		return 0, err
	}
	return io.Copy(f, diff)
}

func (d *GraphDriver) DiffSize(id, parent string) (int64, error) {
	stat, err := os.Stat(d.path(id))
	if err != nil {
		return 0, err
	}
	return stat.Size(), nil
}

func (d *GraphDriver) dir(id string) string {
	return filepath.Join(d.root, filepath.Base(id))
}

func (d *GraphDriver) path(id string) string {
	return filepath.Join(d.dir(id), "diff.tar")
}
