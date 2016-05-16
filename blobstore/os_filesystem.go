package main

import (
	"io"
	"os"
	"path/filepath"

	"github.com/flynn/flynn/pkg/status"
)

type osFile struct {
	*os.File
	os.FileInfo
}

func (f *osFile) Type() string { return "" }
func (f *osFile) ETag() string { return "" }

func NewOSFilesystem(root string) Filesystem {
	return &OSFilesystem{root: root}
}

type OSFilesystem struct {
	root string
}

func (s *OSFilesystem) List(dir string) ([]string, error) {
	f, err := s.Open(dir)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if !f.(*osFile).IsDir() {
		return nil, ErrNotFound
	}
	entries, err := f.(*osFile).Readdir(0)
	if err != nil {
		return nil, err
	}
	paths := make([]string, len(entries))
	for i, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		if entry.IsDir() {
			path = path + "/"
		}
		paths[i] = path
	}
	return paths, nil
}

func (s *OSFilesystem) Open(name string) (File, error) {
	f, err := os.Open(s.path(name))
	if err != nil {
		if os.IsNotExist(err) {
			err = ErrNotFound
		}
		return nil, err
	}
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	return &osFile{File: f, FileInfo: fi}, nil
}

func (s *OSFilesystem) Put(name string, r io.Reader, offset int64, typ string) error {
	path := s.path(name)
	os.MkdirAll(filepath.Dir(path), 0755)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Seek(offset, os.SEEK_SET); err != nil {
		return err
	}
	_, err = io.Copy(f, r)
	return err
}

func (s *OSFilesystem) Copy(dstPath, srcPath string) error {
	src, err := s.Open(srcPath)
	if err != nil {
		return err
	} else if src.(*osFile).IsDir() {
		return ErrNotFound
	}
	defer src.Close()
	return s.Put(dstPath, src, 0, "")
}

func (s *OSFilesystem) Delete(name string) error {
	return os.RemoveAll(s.path(name))
}

func (s *OSFilesystem) path(name string) string {
	return filepath.Join(s.root, name)
}

func (*OSFilesystem) Status() status.Status {
	return status.Healthy
}
