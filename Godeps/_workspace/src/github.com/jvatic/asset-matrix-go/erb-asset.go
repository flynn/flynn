package assetmatrix

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type ERBAsset struct {
	input        Asset
	r            *AssetRoot
	p            string
	indexKey     string
	cacheBreaker string
	erbRBPath    string
}

func (a *ERBAsset) OutputExt() string {
	return filepath.Ext(strings.TrimSuffix(filepath.Base(a.p), ".erb"))
}

func (a *ERBAsset) OutputPath() string {
	p, err := a.RelPath()
	if err != nil {
		log.Fatal(err)
	}
	return strings.TrimSuffix(p, ".erb")
}

func (a *ERBAsset) Open() (*os.File, error) {
	return a.input.Open()
}

func (a *ERBAsset) Initialize() error {
	return nil
}

func (a *ERBAsset) Checksum() string {
	return ""
}

func (a *ERBAsset) Path() string {
	return a.p
}

func (a *ERBAsset) RelPath() (string, error) {
	return filepath.Rel(a.r.Path, a.p)
}

func (a *ERBAsset) SetIndexKey(key string) {
	a.indexKey = key
}

func (a *ERBAsset) IndexKey() string {
	return a.indexKey
}

func (a *ERBAsset) ImportPaths() []string {
	return []string{}
}

func (a *ERBAsset) Compile() (io.Reader, error) {
	data, err := a.input.Compile()
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	var cmd *exec.Cmd
	if _, err := os.Stat("Gemfile"); err == nil {
		cmd = exec.Command("bundle", "exec", "ruby", a.erbRBPath)
	} else {
		cmd = exec.Command("ruby", a.erbRBPath)
	}
	cmd.Stdin = data
	cmd.Stdout = &buf
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, fmt.Sprintf("CACHE_BREAKER=%s", a.cacheBreaker))
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	return &buf, nil
}
