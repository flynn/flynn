package assetmatrix

import (
	"crypto/md5"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	log "github.com/inconshreveable/log15"
)

type Config struct {
	Paths          []*AssetRoot
	Outputs        []string
	OutputDir      string
	CacheDir       string
	AssetURLPrefix string
}

type Matrix struct {
	config            *Config
	cache             *Cache
	cacheBreaker      string
	transformerJSPath string
	scssJSPath        string
	erbRBPath         string
	prevManifest      *Manifest
	Manifest          *Manifest
	Log               log.Logger
}

type Manifest struct {
	mtx    sync.Mutex
	Assets map[string]string `json:"assets"`
}

func New(config *Config) *Matrix {
	cacheDir := config.CacheDir
	if cacheDir == "" {
		cacheDir = filepath.Join(config.OutputDir, ".cache")
	}
	l := log.New("component", "asset-matrix")
	m := &Matrix{
		Log:    l,
		config: config,
		cache:  &Cache{Dir: cacheDir, l: l.New("type", "cache")},
	}
	for _, r := range config.Paths {
		r.findAsset = m.findAsset
		r.assetURLPrefix = m.config.AssetURLPrefix
	}
	return m
}

func (m *Matrix) Build() error {
	defer m.cleanupTempfiles()

	startedAt := time.Now()
	hashData, err := time.Now().MarshalBinary()
	if err != nil {
		return err
	}
	m.cacheBreaker = fmt.Sprintf("%x", md5.Sum(hashData))

	m.prevManifest = m.parsePrevManifest()
	m.Manifest = &Manifest{
		Assets: make(map[string]string, 0),
	}

	m.Log.Info("Installing dependencies...")
	if err := m.installDeps(); err != nil {
		return err
	}
	if err := m.createTempfiles(); err != nil {
		return err
	}
	for _, r := range m.config.Paths {
		r.transformerJSPath = m.transformerJSPath
		r.scssJSPath = m.scssJSPath
		r.erbRBPath = m.erbRBPath
	}

	m.Log.Info("Cloning external repos...")
	for _, r := range m.config.Paths {
		if r.GitRepo != "" {
			if err := r.CloneRepo(); err != nil {
				return err
			}
		}
	}

	m.Log.Info("Validating asset roots...")
	for _, r := range m.config.Paths {
		if _, err := os.Stat(r.Path); err != nil {
			return err
		}
	}

	m.Log.Info("Enumerating assets...")
	if err := m.enumerateAssets(); err != nil {
		return err
	}
	m.Log.Info("Building output trees...")
	trees, err := m.buildOutputTrees()
	if err != nil {
		return err
	}
	if len(m.config.Outputs) != 0 {
		m.Log.Info("Filtering output trees...")
		matchers := make([]*regexp.Regexp, 0, len(m.config.Outputs))
		for _, pattern := range m.config.Outputs {
			pattern = "^" + strings.Replace(strings.Replace(pattern, ".", "\\.", -1), "*", ".*", -1) + "$"
			matchers = append(matchers, regexp.MustCompile(pattern))
		}
		filteredTrees := make([][]Asset, 0, len(matchers))
		for _, t := range trees {
			a := t[len(t)-1]
			name, err := a.RelPath()
			if err != nil {
				return err
			}
			for _, r := range matchers {
				if r.MatchString(name) {
					m.Log.Info(name)
					filteredTrees = append(filteredTrees, t)
					break
				}
			}
		}
		trees = filteredTrees
	}
	m.Log.Info("Compiling output trees...")
	if err := m.compileTrees(trees); err != nil {
		return err
	}

	m.Log.Info("Writing manifest.json...")
	manifestJSONPath := filepath.Join(m.config.OutputDir, "manifest.json")
	f, err := os.Create(manifestJSONPath)
	defer f.Close()
	if err != nil {
		return err
	}
	e := json.NewEncoder(f)
	if err := e.Encode(m.Manifest); err != nil {
		return err
	}

	m.Log.Info("Cleaning up cache dir...")
	if err := m.cache.CleanupCacheDir(); err != nil {
		return err
	}

	duration := time.Since(startedAt)
	m.Log.Info("Completed", "duration", duration)
	return nil
}

func (m *Matrix) parsePrevManifest() *Manifest {
	prevManifest := &Manifest{
		Assets: make(map[string]string, 0),
	}
	file, err := os.Open(filepath.Join(m.config.OutputDir, "manifest.json"))
	if err != nil {
		return prevManifest
	}
	defer file.Close()
	json.NewDecoder(file).Decode(&prevManifest)
	return prevManifest
}

func (m *Matrix) RemoveOldAssets() {
	m.Log.Info("Removing old assets...")
	for logicalPath, path := range m.prevManifest.Assets {
		if m.Manifest.Assets[logicalPath] == path {
			continue
		}
		p := filepath.Join(m.config.OutputDir, path)
		os.Remove(p)
	}
}

func installNpmPackages(names []string) error {
	for _, n := range names {
		if _, err := os.Stat("node_modules/" + strings.Split(n, "@")[0]); err == nil {
			continue
		}
		cmd := exec.Command("npm", "install", n)
		cmd.Stderr = os.Stderr
		cmd.Stdout = os.Stdout
		if err := cmd.Run(); err != nil {
			return err
		}
	}
	return nil
}

func (m *Matrix) installDeps() error {
	return installNpmPackages([]string{
		"recast@0.10.30",
		"es6-promise@3.0.2",
		"node-sass@4.12.0",
		"babel-cli@6.11.4",
		"babel-plugin-transform-react-jsx@6.8",
		"eslint@1.6.0",
		"eslint-plugin-react@3.5.1",
	})
}

func (m *Matrix) createTempfiles() error {
	f, err := os.Create("transformer.js")
	if err != nil {
		return err
	}
	m.transformerJSPath = f.Name()
	if _, err := f.WriteString(transformerJS); err != nil {
		return err
	}

	f, err = os.Create("scss.js")
	if err != nil {
		return err
	}
	m.scssJSPath = f.Name()
	if _, err := f.WriteString(scssJS); err != nil {
		return err
	}

	f, err = os.Create("erb.rb")
	if err != nil {
		return err
	}
	m.erbRBPath = f.Name()
	if _, err := f.WriteString(erbRB); err != nil {
		return err
	}

	return nil
}

func (m *Matrix) cleanupTempfiles() {
	os.Remove(m.transformerJSPath)
	os.Remove(m.scssJSPath)
	os.Remove(m.erbRBPath)
}

func (m *Matrix) enumerateAssets() error {
	errChan := make(chan error)
	enumerateAssets := func(r *AssetRoot) {
		errChan <- r.enumerateAssets()
	}
	for _, r := range m.config.Paths {
		r.SetCacheBreaker(m.cacheBreaker)
		r.Log = m.Log
		go enumerateAssets(r)
	}
	for _ = range m.config.Paths {
		if err := <-errChan; err != nil {
			return err
		}
	}
	return nil
}

func (m *Matrix) buildOutputTrees() ([][]Asset, error) {
	errChan := make(chan error)
	treesChan := make(chan [][]Asset)
	trees := [][]Asset{}
	buildOutputTrees := func(r *AssetRoot) {
		trees, err := r.buildOutputTrees()
		go func() {
			errChan <- err
		}()
		go func() {
			treesChan <- trees
		}()
	}
	for _, r := range m.config.Paths {
		go buildOutputTrees(r)
	}
	for _ = range m.config.Paths {
		if err := <-errChan; err != nil {
			return nil, err
		}
		for _, t := range <-treesChan {
			trees = append(trees, t)
		}
	}
	return trees, nil
}

func (m *Matrix) compileTrees(trees [][]Asset) error {
	errChan := make(chan error)
	compileTree := func(t []Asset) {
		errChan <- m.compileTree(t)
	}
	for _, t := range trees {
		go compileTree(t)
	}
	for _ = range trees {
		if err := <-errChan; err != nil {
			return err
		}
	}
	return nil
}

var AssetNotFoundError = errors.New("Asset not found")

func (m *Matrix) findAsset(key string) (Asset, error) {
	for _, r := range m.config.Paths {
		k := key
		rootBasePath, _ := filepath.Abs(r.Path)
		if relPath, err := filepath.Rel(rootBasePath, key); err == nil {
			k = relPath
		}
		if a, ok := r.assetIndex[k]; ok {
			return a, nil
		}
	}
	m.Log.Info("Asset not found", "key", key)
	return nil, AssetNotFoundError
}

func (m *Matrix) compileTree(tree []Asset) error {
	type result struct {
		io.Reader
		error
		idx int
	}
	results := make(chan result)
	compileAsset := func(i int, a Asset) {
		if cachedAsset, err := m.cache.FindCachedAsset(a); err == nil {
			r, err := cachedAsset.Compile()
			if err == nil {
				results <- result{r, err, i}
				return
			}
		}
		r, err := a.Compile()
		if err == nil {
			r = m.cache.CacheAsset(r, a.Checksum())
		}
		results <- result{r, err, i}
	}
	for i, a := range tree {
		go compileAsset(i, a)
	}
	readers := make([]io.Reader, len(tree))
	for _ = range tree {
		res := <-results
		if res.error != nil {
			return res.error
		}
		readers[res.idx] = res.Reader
	}
	outputPath := tree[len(tree)-1].OutputPath()
	manifestKey := outputPath
	ext := filepath.Ext(outputPath)
	outputPath = strings.TrimSuffix(outputPath, ext) + "-" + m.cacheBreaker + ext
	manifestVal := outputPath
	outputPath = filepath.Join(m.config.OutputDir, outputPath)
	if err := os.MkdirAll(filepath.Dir(outputPath), os.ModePerm); err != nil {
		return err
	}
	file, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	m.Manifest.mtx.Lock()
	m.Manifest.Assets[manifestKey] = manifestVal
	m.Manifest.mtx.Unlock()
	m.Log.Info("Writing output", "path", outputPath)
	defer file.Close()
	var offset int64
	for i, r := range readers {
		if i > 0 {
			n, err := file.WriteAt([]byte("\n"), offset)
			if err != nil {
				return err
			}
			offset += int64(n)
		}
		n, err := io.Copy(file, r)
		if err != nil {
			return err
		}
		offset += n
	}
	return nil
}
