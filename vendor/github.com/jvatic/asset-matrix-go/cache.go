package assetmatrix

import (
	"io"
	"os"
	"path/filepath"
	"sync"

	log "github.com/inconshreveable/log15"
)

type Cache struct {
	Dir            string
	cacheHits      []string
	cacheHitsMutex sync.Mutex
	l              log.Logger
}

func (c *Cache) FindCachedAsset(a Asset) (Asset, error) {
	checksum := a.Checksum()
	if checksum == "" {
		return a, nil
	}
	p := filepath.Join(c.Dir, checksum)
	if _, err := os.Lstat(p); err != nil {
		return nil, err
	}
	c.cacheHitsMutex.Lock()
	c.cacheHits = append(c.cacheHits, p)
	c.cacheHitsMutex.Unlock()
	return &CachedAsset{input: a, p: p, l: c.l.New("path", p)}, nil
}

func (c *Cache) CacheAsset(ar io.Reader, checksum string) io.Reader {
	if checksum == "" {
		return ar
	}
	p := filepath.Join(c.Dir, checksum)
	if err := os.MkdirAll(c.Dir, 0755); err != nil {
		// abort
		c.l.Error("Error creating cache dir", "err", err)
		return ar
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY, 0755)
	if err != nil {
		// abort
		c.l.Error("Error creating cache file", "err", err, "checksum", checksum)
		return ar
	}
	c.cacheHitsMutex.Lock()
	c.cacheHits = append(c.cacheHits, p)
	c.cacheHitsMutex.Unlock()
	cr, w := io.Pipe()
	r := io.TeeReader(ar, w)
	go func() {
		defer f.Close()
		defer w.Close()
		if _, err := io.Copy(f, cr); err != nil {
			c.l.Error("Error writing to cache", "err", err)
		}
	}()
	return r
}

func (c *Cache) CleanupCacheDir() error {
	c.cacheHitsMutex.Lock()
	defer c.cacheHitsMutex.Unlock()
	return filepath.Walk(c.Dir, func(path string, info os.FileInfo, err error) error {
		if path == c.Dir {
			return nil
		}
		found := false
		for _, p := range c.cacheHits {
			if p == path {
				found = true
				break
			}
		}
		if !found {
			return os.Remove(path)
		}
		return nil
	})
}

type CachedAsset struct {
	p     string
	input Asset
	l     log.Logger
}

func (a *CachedAsset) Open() (*os.File, error) {
	return os.Open(a.p)
}

func (a *CachedAsset) Initialize() error {
	return nil
}

func (a *CachedAsset) Checksum() string {
	return a.input.Checksum()
}

func (a *CachedAsset) Path() string {
	return a.input.Path()
}

func (a *CachedAsset) RelPath() (string, error) {
	return a.input.RelPath()
}

func (a *CachedAsset) SetIndexKey(key string) {
	a.input.SetIndexKey(key)
}

func (a *CachedAsset) IndexKey() string {
	return a.input.IndexKey()
}

func (a *CachedAsset) ImportPaths() []string {
	return a.input.ImportPaths()
}

func (a *CachedAsset) Compile() (io.Reader, error) {
	file, err := os.Open(a.p)
	if err != nil {
		return nil, err
	}
	r, w := io.Pipe()
	go func() {
		defer file.Close()
		defer w.Close()
		if _, err := io.Copy(w, file); err != nil {
			a.l.Error("Error reading from cache", "err", err)
		}
	}()
	return r, nil
}

func (a *CachedAsset) OutputExt() string {
	return a.input.OutputExt()
}

func (a *CachedAsset) OutputPath() string {
	return a.input.OutputPath()
}
