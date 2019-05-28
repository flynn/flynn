package assetmatrix

import (
	"crypto/md5"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	log "github.com/inconshreveable/log15"
)

type AssetRoot struct {
	Path              string
	GitRepo           string
	GitBranch         string
	GitRef            string
	ESLintConfigPath  string
	Log               log.Logger
	cacheBreaker      string
	indexMtx          sync.Mutex
	assetIndex        map[string]Asset
	findAsset         func(string) (Asset, error)
	transformerJSPath string
	scssJSPath        string
	erbRBPath         string
	assetURLPrefix    string
}

func (r *AssetRoot) SetCacheBreaker(cacheBreaker string) {
	r.cacheBreaker = cacheBreaker
}

func (r *AssetRoot) CloneRepo() error {
	var cmd *exec.Cmd
	repoHash := md5.Sum([]byte(r.GitRepo))
	if err := os.MkdirAll(".gitrepos", os.ModePerm); err != nil {
		return err
	}
	cloneDir := filepath.Join(".gitrepos", fmt.Sprintf("%x", repoHash))
	r.Path = filepath.Join(cloneDir, r.Path)
	if _, err := os.Stat(cloneDir); err != nil {
		cmd = exec.Command("git", "clone", r.GitRepo, "--branch", r.GitBranch, cloneDir)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return err
		}
	}
	if r.GitRef == "" {
		return nil
	}
	cmd = exec.Command("bash", "-c", strings.Join([]string{
		"cd", cloneDir, "&&", "git", "checkout", r.GitRef,
		"&&", "cd", "../../",
	}, " "))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (r *AssetRoot) enumerateAssets() error {
	var walkFunc filepath.WalkFunc
	errChan := make(chan error)
	numAssets := 0
	initAsset := func(a Asset) {
		errChan <- a.Initialize()
	}
	r.assetIndex = make(map[string]Asset, 0)
	walkFunc = func(path string, info os.FileInfo, err error) error {
		if info.IsDir() {
			return nil
		}
		numAssets++
		a := NewAsset(r, path)
		r.indexAsset(a)
		go initAsset(a)
		return nil
	}
	filepath.Walk(r.Path, walkFunc)
	for i := 0; i < numAssets; i++ {
		if err := <-errChan; err != nil {
			return err
		}
	}
	return nil
}

func (r *AssetRoot) indexAsset(a Asset) error {
	r.indexMtx.Lock()
	defer r.indexMtx.Unlock()
	rel, err := filepath.Rel(r.Path, a.Path())
	if err != nil {
		return err
	}
	a.SetIndexKey(rel)
	r.assetIndex[rel] = a
	return nil
}

func (r *AssetRoot) buildOutputTrees() ([][]Asset, error) {
	trees := [][]Asset{}
	var buildTree func(*[]Asset, Asset) error
	buildTree = func(t *[]Asset, a Asset) error {
		key := a.IndexKey()
		for _, i := range *t {
			if i.IndexKey() == key {
				return nil
			}
		}
		for _, p := range a.ImportPaths() {
			ia, err := r.findAsset(p)
			if err != nil {
				return err
			}
			if err := buildTree(t, ia); err != nil {
				return err
			}
		}
		*t = append(*t, a)
		return nil
	}
	for _, a := range r.assetIndex {
		t := []Asset{}
		if err := buildTree(&t, a); err != nil {
			return nil, err
		}
		trees = append(trees, t)
	}
	return trees, nil
}
