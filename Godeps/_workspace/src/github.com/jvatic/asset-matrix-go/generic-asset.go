package assetmatrix

import (
	"bytes"
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
	defer file.Close()
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, file); err != nil {
		return nil, err
	}
	return &buf, nil
}
