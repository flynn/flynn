package tufutil

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"time"

	tuf "github.com/flynn/go-tuf/client"
)

var DefaultHTTPRetries = &tuf.HTTPRemoteRetries{
	Delay: time.Second,
	Total: 30 * time.Second,
}

func Download(client *tuf.Client, path string) (io.ReadCloser, error) {
	tmp, err := NewTempFile()
	if err != nil {
		return nil, err
	}
	if err := client.Download(path, tmp); err != nil {
		return nil, err
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	return tmp, nil
}

func DownloadString(client *tuf.Client, path string) (string, error) {
	rc, err := Download(client, path)
	if err != nil {
		return "", err
	}
	defer rc.Close()
	data, err := ioutil.ReadAll(rc)
	if err != nil {
		return "", err
	}
	return string(data), nil
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

// GetVersion returns the given target's version from custom metadata
func GetVersion(client *tuf.Client, name string) (string, error) {
	targets, err := client.Targets()
	if err != nil {
		return "", err
	}
	target, ok := targets[name]
	if !ok {
		return "", fmt.Errorf("missing %q in tuf targets", name)
	}
	if target.Custom == nil || len(*target.Custom) == 0 {
		return "", errors.New("missing custom metadata in tuf target")
	}
	var data struct {
		Version string
	}
	json.Unmarshal(*target.Custom, &data)
	if data.Version == "" {
		return "", errors.New("missing version in tuf target")
	}
	return data.Version, nil
}
