package assetmatrix

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

type JavaScriptAsset struct {
	r                 *AssetRoot
	p                 string
	input             Asset
	indexKey          string
	importPaths       []string
	importMapping     map[string]string
	hasExports        bool
	compileMtx        sync.Mutex
	compiledData      bytes.Buffer
	isCompiled        bool
	transformerJSPath string
}

func (a *JavaScriptAsset) OutputExt() string {
	return ".js"
}

func (a *JavaScriptAsset) OutputPath() string {
	p, err := a.RelPath()
	if err != nil {
		log.Fatal(err)
	}
	return p
}

func (a *JavaScriptAsset) Open() (*os.File, error) {
	return a.input.Open()
}

func (a *JavaScriptAsset) Path() string {
	return a.p
}

func (a *JavaScriptAsset) RelPath() (string, error) {
	return filepath.Rel(a.r.Path, a.p)
}

func (a *JavaScriptAsset) SetIndexKey(k string) {
	a.indexKey = k
}

func (a *JavaScriptAsset) IndexKey() string {
	return a.indexKey
}

func (a *JavaScriptAsset) ImportPaths() []string {
	return a.importPaths
}

func (a *JavaScriptAsset) Initialize() error {
	var importPaths []string
	importMapping := make(map[string]string, 0)
	file, err := a.Open()
	defer file.Close()
	if err != nil {
		return err
	}
	s := bufio.NewScanner(file)
	for s.Scan() {
		if jsImportRegex.Match(s.Bytes()) {
			n, p, err := a.parseImport(s.Bytes())
			if err != nil {
				return err
			}
			importPaths = append(importPaths, p)
			importMapping[n] = p
		} else if jsExportRegex.Match(s.Bytes()) {
			a.hasExports = true
		}
	}
	if err := s.Err(); err != nil {
		return err
	}
	a.importPaths = importPaths
	a.importMapping = importMapping
	return nil
}

var jsImportParseRegex = regexp.MustCompile("from[^'\"]+(['\"])([^'\"]+)['\"]")
var jsImportRegex = regexp.MustCompile("^import .*$")
var jsExportRegex = regexp.MustCompile("^export .*$")

func (a *JavaScriptAsset) parseImport(line []byte) (string, string, error) {
	if !jsImportParseRegex.Match(line) {
		return "", "", fmt.Errorf("Invalid import statement: %s", string(line))
	}
	p := string(jsImportParseRegex.FindAllSubmatch(line, 4)[0][2])
	if filepath.IsAbs(p) {
		return p, p, nil
	}
	if !strings.HasPrefix(p, ".") {
		return p, p + filepath.Ext(a.p), nil
	}
	n := p
	if filepath.Ext(p) == "" {
		p = p + filepath.Ext(a.p)
	}
	p, err := filepath.Rel(a.r.Path, filepath.Join(filepath.Dir(a.p), p))
	return n, p, err
}

func (a *JavaScriptAsset) Compile() (io.Reader, error) {
	a.compileMtx.Lock()
	defer a.compileMtx.Unlock()

	if len(a.importPaths) == 0 && !a.hasExports {
		return a.input.Compile()
	}

	if a.isCompiled {
		return bytes.NewReader(a.compiledData.Bytes()), nil
	}

	data, err := a.input.Compile()
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	cmd := exec.Command("node", a.transformerJSPath)
	cmd.Stdin = data
	cmd.Stdout = &buf
	// cmd.Stderr = os.Stderr
	importMapping, err := json.Marshal(a.importMapping)
	cmd.Env = []string{
		`MODULES_GLOBAL_VAR_NAME=this.__modules`,
		`MODULES_LOCAL_VAR_NAME=__m`,
		fmt.Sprintf(`MODULE_NAME=%s`, a.IndexKey()),
		fmt.Sprintf(`IMPORT_MAPPING=%s`, string(importMapping)),
	}
	if err := cmd.Run(); err != nil {
		return nil, err
	}

	a.compiledData = buf
	a.isCompiled = true

	return bytes.NewReader(a.compiledData.Bytes()), nil
}
