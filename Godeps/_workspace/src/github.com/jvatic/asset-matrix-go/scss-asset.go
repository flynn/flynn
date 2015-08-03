package assetmatrix

import (
	"bufio"
	"bytes"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type SCSSAsset struct {
	input          Asset
	r              *AssetRoot
	p              string
	indexKey       string
	findAsset      func(string) (Asset, error)
	scssJSPath     string
	assetURLPrefix string
}

func (a *SCSSAsset) OutputExt() string {
	return ".css"
}

func (a *SCSSAsset) OutputPath() string {
	p, err := a.RelPath()
	if err != nil {
		log.Fatal(err)
	}
	if filepath.Ext(p) == ".scss" {
		p = strings.TrimSuffix(p, ".scss")
	}
	if filepath.Ext(p) != ".css" {
		return p + ".css"
	}
	return p
}

func (a *SCSSAsset) Open() (*os.File, error) {
	return a.input.Open()
}

func (a *SCSSAsset) Initialize() error {
	return nil
}

func (a *SCSSAsset) Path() string {
	return a.p
}

func (a *SCSSAsset) RelPath() (string, error) {
	return filepath.Rel(a.r.Path, a.p)
}

func (a *SCSSAsset) SetIndexKey(key string) {
	a.indexKey = key
}

func (a *SCSSAsset) IndexKey() string {
	return a.indexKey
}

func (a *SCSSAsset) ImportPaths() []string {
	return []string{}
}

func (a *SCSSAsset) Compile() (io.Reader, error) {
	data, err := a.input.Compile()
	if err != nil {
		return nil, err
	}

	t, err := ioutil.TempFile("", filepath.Base(a.Path()))
	if err != nil {
		return nil, err
	}
	if _, err := io.Copy(t, data); err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	cmd := exec.Command("node", a.scssJSPath)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()

	errChan := make(chan error)
	go func() {
		s := bufio.NewScanner(stdout)
		for s.Scan() {
			line := s.Text()
			if line == "<data>" {
				stdin.Write([]byte(t.Name() + "\n"))
			} else if line == "<assetRoot>" {
				stdin.Write([]byte(a.r.Path + "\n"))
			} else if strings.HasPrefix(line, "<assetPath>:") {
				name := strings.TrimPrefix(line, "<assetPath>:")
				if strings.HasPrefix(name, ".") {
					name, err = filepath.Rel(a.r.Path, filepath.Join(filepath.Dir(a.p), name))
					if err != nil {
						errChan <- err
						return
					}
				}
				var ia Asset
				if filepath.Ext(name) != "" {
					ia, err = a.findAsset(name)
				} else {
					ia, err = a.findAsset(name + ".scss")
					if err == AssetNotFoundError {
						ia, err = a.findAsset(name + ".css")
					}
				}
				if err != nil {
					errChan <- err
					return
				}
				if _, err := stdin.Write([]byte(ia.Path() + "\n")); err != nil {
					errChan <- err
					return
				}
			} else if strings.HasPrefix(line, "<assetOutputPath>:") {
				ia, err := a.findAsset(strings.Split(strings.Split(strings.TrimPrefix(line, "<assetOutputPath>:"), "?")[0], "#")[0])
				if err != nil {
					errChan <- err
					return
				}
				_p, err := ia.RelPath()
				if err != nil {
					log.Fatal(err)
				}
				_p = strings.TrimSuffix(_p, filepath.Ext(_p)) + "-" + a.r.cacheBreaker + filepath.Ext(_p)
				if _, err := stdin.Write([]byte(_p + "\n")); err != nil {
					errChan <- err
					return
				}
			} else if line == "<output>" {
				break
			}
		}
		if _, err := io.Copy(&buf, stdout); err != nil {
			errChan <- s.Err()
		} else {
			errChan <- s.Err()
		}
	}()

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	if err := <-errChan; err != nil {
		return nil, err
	}

	if err := cmd.Wait(); err != nil {
		return nil, err
	}

	os.Remove(t.Name())

	return &buf, nil
}
