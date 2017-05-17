package assetmatrix

import (
	"crypto/md5"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"

	log "gopkg.in/inconshreveable/log15.v2"
)

type GenericAsset struct {
	r        *AssetRoot
	p        string
	indexKey string
	checkSum string
	l        log.Logger
}

func (a *GenericAsset) OutputExt() string {
	return filepath.Ext(a.p)
}

func (a *GenericAsset) OutputPath() string {
	p, err := a.RelPath()
	if err != nil {
		a.l.Error("Error getting rel path", "err", err)
		os.Exit(1)
	}
	return p
}

func (a *GenericAsset) Open() (*os.File, error) {
	return os.Open(a.p)
}

func (a *GenericAsset) Initialize() error {
	return nil
}

func (a *GenericAsset) Checksum() string {
	if a.checkSum != "" {
		return a.checkSum
	}
	file, err := a.Open()
	defer file.Close()
	if err != nil {
		return ""
	}
	h := md5.New()
	if _, err := io.Copy(h, file); err != nil {
		return ""
	}
	h.Write([]byte(a.p))
	a.checkSum = hex.EncodeToString(h.Sum(nil))
	return a.checkSum
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
			a.l.Error("Error writing output", "err", err)
			os.Exit(1)
		}
	}()
	return r, nil
}
