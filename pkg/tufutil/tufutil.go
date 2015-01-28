package tufutil

import (
	"io"
	"io/ioutil"
	"os"

	tuf "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-tuf/client"
)

func Download(client *tuf.Client, path string) (io.ReadCloser, error) {
	tmp, err := NewTempFile()
	if err != nil {
		return nil, err
	}
	if err := client.Download(path, tmp); err != nil {
		return nil, err
	}
	if _, err := tmp.Seek(0, os.SEEK_SET); err != nil {
		return nil, err
	}
	return tmp, nil
}

func NewTempFile() (*TempFile, error) {
	file, err := ioutil.TempFile("", "flynn-tuf")
	if err != nil {
		return nil, err
	}
	return &TempFile{file}, nil
}

type TempFile struct {
	*os.File
}

func (t *TempFile) Delete() error {
	t.File.Close()
	return os.Remove(t.Name())
}

func (t *TempFile) Close() error {
	return t.Delete()
}
