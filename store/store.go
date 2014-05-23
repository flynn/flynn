package store

import (
	"encoding/json"
	"errors"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/dotcloud/docker/archive"
	"github.com/dotcloud/docker/daemon/graphdriver"
	"github.com/flynn/docker-pull/registry"
)

type Store struct {
	Root   string
	driver graphdriver.Driver
}

func New(root string, driver graphdriver.Driver) (*Store, error) {
	path, err := filepath.Abs(filepath.Join(root, "graph"))
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(path, "_tmp"), 0700); err != nil {
		return nil, err
	}

	return &Store{Root: path, driver: driver}, nil
}

var ErrExists = errors.New("store: image exists")

func (s *Store) Add(img *registry.Image) (err error) {
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

	layer, err := s.driver.Get(img.ID, "")
	if err != nil {
		return err
	}
	defer s.driver.Put(img.ID)

	if differ, ok := s.driver.(graphdriver.Differ); ok {
		if err := differ.ApplyDiff(img.ID, img); err != nil {
			return err
		}
	} else {
		if err := archive.ApplyLayer(layer, img); err != nil {
			return err
		}
	}

	if err := ioutil.WriteFile(filepath.Join(tmp, "layersize"), []byte{'0'}, 0600); err != nil {
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

func (s *Store) root(id string) string {
	return filepath.Join(s.Root, id)
}

func (s *Store) tempDir() (string, error) {
	return ioutil.TempDir(filepath.Join(s.Root, "_tmp"), "")
}
