package assetmatrix

import (
	"io"
	"log"
	"os"
	"path/filepath"
)

type GenericAsset struct {
	r        *AssetRoot
	p        string
	indexKey string
}

func (a *GenericAsset) OutputExt() string {
	return filepath.Ext(a.p)
}

func (a *GenericAsset) OutputPath() string {
	p, err := a.RelPath()
	if err != nil {
		log.Fatal(err)
	}
	return p
}

func (a *GenericAsset) Open() (*os.File, error) {
	return os.Open(a.p)
}

func (a *GenericAsset) Initialize() error {
	return nil
}

func (a *GenericAsset) Path() string {
	return a.p
}

func (a *GenericAsset) RelPath() (string, error) {
	return filepath.Rel(a.r.Path, a.p)
}

func (a *GenericAsset) SetIndexKey(k string) {
	a.indexKey = k
}

func (a *GenericAsset) IndexKey() string {
	return a.indexKey
}

func (a *GenericAsset) ImportPaths() []string {
	return []string{}
}

func (a *GenericAsset) Compile() (io.Reader, error) {
	file, err := os.Open(a.p)
	if err != nil {
		return nil, err
	}
	r, w := io.Pipe()
	go func() {
		defer file.Close()
		defer w.Close()
		if _, err := io.Copy(w, file); err != nil {
			log.Fatal(err)
		}
	}()
	return r, nil
}
