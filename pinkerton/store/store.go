package store

import (
	"encoding/json"
	"errors"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/docker/docker/daemon/graphdriver"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/docker/docker/pkg/archive"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/docker/docker/pkg/tarsum"
	"github.com/flynn/flynn/pinkerton/registry"
)

type Store struct {
	Root   string
	driver graphdriver.Driver
	locks  map[string]*os.File
	mtx    sync.Mutex
}

func New(root string, driver graphdriver.Driver) (*Store, error) {
	path, err := filepath.Abs(filepath.Join(root, "graph"))
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(path, "_tmp"), 0700); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(path, "_locks"), 0700); err != nil {
		return nil, err
	}

	return &Store{Root: path, driver: driver, locks: make(map[string]*os.File)}, nil
}

var ErrExists = errors.New("store: image exists")
var ErrChecksumFailed = errors.New("store: checksum failed")

func (s *Store) Add(img *registry.Image, checksum string) (err error) {
	if err := s.lock(img.ID); err != nil {
		return err
	}
	defer s.unlock(img.ID)

	if s.Exists(img.ID) {
		return ErrExists
	}

	defer func() {
		if err != nil {
			// cleanup if we fail
			s.driver.Remove(img.ID)
		}
	}()

	tmp, err := s.tempDir()
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	if err := s.driver.Create(img.ID, img.ParentID); err != nil {
		return err
	}

	var layer io.Reader = img
	if checksum != "" {
		a, err := archive.DecompressStream(img)
		if err != nil {
			return err
		}
		defer a.Close()
		layer, err = tarsum.NewTarSum(a, false, tarsum.Version0)
		if err != nil {
			return err
		}
	}
	size, err := s.driver.ApplyDiff(img.ID, img.ParentID, layer)
	if err != nil {
		return err
	}
	if checksum != "" && layer.(tarsum.TarSum).Sum(nil) != checksum {
		return ErrChecksumFailed
	}

	if err := ioutil.WriteFile(filepath.Join(tmp, "layersize"), strconv.AppendInt(nil, size, 10), 0600); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(tmp, "json"), os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := json.NewEncoder(f).Encode(img); err != nil {
		return err
	}

	return os.Rename(tmp, s.root(img.ID))
}

func (s *Store) Exists(id string) bool {
	_, err := os.Stat(s.root(id))
	return err == nil
}

type Image struct {
	ID     string `json:"id"`
	Parent string `json:"parent"`
}

func (s *Store) WalkHistory(id string, f func(*Image) error) error {
	img, err := s.Get(id)
	if err != nil {
		return err
	}
	for {
		if err := f(img); err != nil {
			return err
		}
		if img.Parent == "" {
			break
		}
		img, err = s.Get(img.Parent)
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) Checksum(img *Image) (string, error) {
	diff, err := s.driver.Diff(img.ID, img.Parent)
	if err != nil {
		return "", err
	}
	defer diff.Close()
	t, err := tarsum.NewTarSum(diff, false, tarsum.Version0)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(ioutil.Discard, t); err != nil {
		return "", err
	}
	return t.Sum(nil), nil
}

func (s *Store) Get(id string) (*Image, error) {
	f, err := os.Open(filepath.Join(s.root(id), "json"))
	if err != nil {
		return nil, err
	}
	defer f.Close()

	img := &Image{}
	if err := json.NewDecoder(f).Decode(img); err != nil {
		return nil, err
	}
	return img, nil
}

func (s *Store) root(id string) string {
	return filepath.Join(s.Root, id)
}

func (s *Store) tempDir() (string, error) {
	return ioutil.TempDir(filepath.Join(s.Root, "_tmp"), "")
}

func (s *Store) lock(id string) error {
	f, err := os.Create(filepath.Join(s.Root, "_locks", id))
	if err != nil {
		return err
	}
	s.mtx.Lock()
	if existing, ok := s.locks[id]; ok {
		go f.Close()
		f = existing
	} else {
		s.locks[id] = f
	}
	s.mtx.Unlock()
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
}

func (s *Store) unlock(id string) error {
	s.mtx.Lock()
	f := s.locks[id]
	delete(s.locks, id)
	s.mtx.Unlock()
	defer f.Close()
	return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}
