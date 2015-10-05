package assetmatrix

import (
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
)

type ESLintAsset struct {
	input    Asset
	r        *AssetRoot
	p        string
	indexKey string
}

func (a *ESLintAsset) OutputExt() string {
	return filepath.Ext(a.p)
}

func (a *ESLintAsset) OutputPath() string {
	p, err := a.RelPath()
	if err != nil {
		log.Fatal(err)
	}
	return p
}

func (a *ESLintAsset) Open() (*os.File, error) {
	return a.input.Open()
}

func (a *ESLintAsset) Initialize() error {
	return nil
}

func (a *ESLintAsset) Path() string {
	return a.p
}

func (a *ESLintAsset) RelPath() (string, error) {
	return filepath.Rel(a.r.Path, a.p)
}

func (a *ESLintAsset) SetIndexKey(k string) {
	a.indexKey = k
}

func (a *ESLintAsset) IndexKey() string {
	return a.indexKey
}

func (a *ESLintAsset) ImportPaths() []string {
	return []string{}
}

func (a *ESLintAsset) Compile() (io.Reader, error) {
	cmd := exec.Command("node_modules/eslint/bin/eslint.js", "--config", a.r.ESLintConfigPath, a.p)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	return a.input.Compile()
}
