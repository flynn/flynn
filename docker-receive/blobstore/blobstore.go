package blobstore

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/docker/distribution/context"
	storage "github.com/docker/distribution/registry/storage/driver"
	"github.com/docker/distribution/registry/storage/driver/base"
	registry "github.com/docker/distribution/registry/storage/driver/factory"
)

const (
	DriverName = "blobstore"

	blobstorePrefix = "/docker-receive"
)

func init() {
	registry.Register(DriverName, factory{})
}

// factory implements the registry.StorageDriverFactory interface
type factory struct{}

func (factory) Create(_ map[string]interface{}) (storage.StorageDriver, error) {
	return NewDriver(), nil
}

type baseEmbed struct {
	base.Base
}

type Driver struct {
	baseEmbed
}

func NewDriver() *Driver {
	return &Driver{
		baseEmbed: baseEmbed{
			Base: base.Base{
				StorageDriver: &driver{},
			},
		},
	}
}

// driver implements the storage.StorageDriver interface using the blobstore
// for storage
type driver struct{}

func (d *driver) Name() string {
	return DriverName
}

func (d *driver) GetContent(ctx context.Context, path string) ([]byte, error) {
	body, err := d.ReadStream(ctx, path, 0)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	return ioutil.ReadAll(body)
}

func (d *driver) PutContent(ctx context.Context, path string, content []byte) error {
	_, err := d.WriteStream(ctx, path, 0, bytes.NewReader(content))
	return err
}

func (d *driver) ReadStream(ctx context.Context, path string, offset int64) (io.ReadCloser, error) {
	req, err := http.NewRequest("GET", d.blobstoreURL(path), nil)
	if err != nil {
		return nil, err
	}
	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusPartialContent {
		res.Body.Close()
		if res.StatusCode == http.StatusNotFound {
			return nil, storage.PathNotFoundError{Path: path}
		}
		return nil, fmt.Errorf("unexpected HTTP status from blobstore: %s", res.Status)
	}
	return res.Body, nil
}

// readCounter wraps an io.Reader and counts how many bytes are read
type readCounter struct {
	r io.Reader
	n int64
}

func (r *readCounter) Read(p []byte) (int, error) {
	n, err := r.r.Read(p)
	atomic.AddInt64(&r.n, int64(n))
	return n, err
}

func (d *driver) WriteStream(ctx context.Context, path string, offset int64, reader io.Reader) (int64, error) {
	// use a readCounter so we can return how many bytes were uploaded
	// from the given reader
	r := &readCounter{r: reader}
	req, err := http.NewRequest("PUT", d.blobstoreURL(path), r)
	if err != nil {
		return 0, err
	}
	if offset > 0 {
		req.Header.Set("Blobstore-Offset", fmt.Sprintf("%d", offset))
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("unexpected HTTP status from blobstore: %s", res.Status)
	}
	return r.n, nil
}

func (d *driver) Stat(ctx context.Context, path string) (storage.FileInfo, error) {
	req, err := http.NewRequest("HEAD", d.blobstoreURL(path), nil)
	if err != nil {
		return nil, err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, storage.PathNotFoundError{Path: path}
	}
	lastMod, err := time.Parse(time.RFC1123, res.Header.Get("Last-Modified"))
	if err != nil {
		return nil, err
	}
	f := storage.FileInfoFields{
		Path:    path,
		IsDir:   false,
		Size:    res.ContentLength,
		ModTime: lastMod,
	}
	return storage.FileInfoInternal{FileInfoFields: f}, nil
}

func (d *driver) List(ctx context.Context, path string) ([]string, error) {
	res, err := http.Get("http://blobstore.discoverd/?dir=" + d.blobstorePath(path))
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected HTTP status from blobstore: %s", res.Status)
	}
	var fullPaths []string
	if err := json.NewDecoder(res.Body).Decode(&fullPaths); err != nil {
		return nil, err
	}
	paths := make([]string, len(fullPaths))
	for i, path := range fullPaths {
		paths[i] = strings.TrimPrefix(path, blobstorePrefix)
	}
	return paths, nil
}

func (d *driver) Move(ctx context.Context, src, dst string) error {
	req, err := http.NewRequest("PUT", d.blobstoreURL(dst), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Blobstore-Copy-From", d.blobstorePath(src))
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return storage.PathNotFoundError{Path: src}
	} else if res.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected HTTP status from blobstore: %s", res.Status)
	}
	return d.Delete(ctx, src)
}

func (d *driver) Delete(ctx context.Context, path string) error {
	req, err := http.NewRequest("DELETE", d.blobstoreURL(path), nil)
	if err != nil {
		return err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected HTTP status from blobstore: %s", res.Status)
	}
	return nil
}

func (d *driver) URLFor(ctx context.Context, path string, options map[string]interface{}) (string, error) {
	return "", storage.ErrUnsupportedMethod
}

func (d *driver) blobstoreURL(path string) string {
	return "http://blobstore.discoverd" + d.blobstorePath(path)
}

func (d *driver) blobstorePath(path string) string {
	return blobstorePrefix + path
}
