package main

import (
	"io"
	"os"
	"path/filepath"
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

func (s *OSFilesystem) Put(name string, r io.Reader, typ string) error {
	path := s.path(name)
	os.MkdirAll(filepath.Dir(path), 0755)
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, r)
	return err
}

func (s *OSFilesystem) Delete(name string) error {
	return os.RemoveAll(s.path(name))
}

func (s *OSFilesystem) path(name string) string {
	return filepath.Join(s.root, name)
}
