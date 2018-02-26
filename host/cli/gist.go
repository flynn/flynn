package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"time"

	"github.com/cheggaaa/pb"
	"github.com/docker/docker/pkg/term"
	"github.com/inconshreveable/log15"
)

// Actual limit is likely ~200mb.
const GistMaxSize = 195 * 1024 * 1024

type Gist struct {
	URL         string          `json:"html_url,omitempty"`
	Description string          `json:"description"`
	Public      bool            `json:"public"`
	Files       map[string]File `json:"files"`
	Size        int
}

func (g *Gist) AddLocalFile(name, filepath string) error {
	content, err := ioutil.ReadFile(filepath)
	if err != nil {
		return err
	}
	g.AddFile(name, string(content))
	return nil
}

func (g *Gist) AddFile(name, content string) {
	g.Files[name] = File{Content: content}
	g.Size = g.Size + len(content)
}

func (g *Gist) Upload(log log15.Logger) error {
	if len(g.Files) == 0 {
		return errors.New("cannot create empty gist")
	}

	payload, err := json.Marshal(g)
	if err != nil {
		log.Error("error preparing gist content", "err", err)
		return err
	}
	var body io.Reader = bytes.NewReader(payload)
	if term.IsTerminal(os.Stderr.Fd()) {
		bar := pb.New(len(payload))
		bar.SetUnits(pb.U_BYTES)
		bar.ShowSpeed = true
		bar.Output = os.Stderr
		bar.Start()
		defer bar.Finish()
		body = bar.NewProxyReader(body)
	}
	req, err := http.NewRequest("POST", "https://api.github.com/gists", body)
	if err != nil {
		log.Error("error preparing HTTP request", "err", err)
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	log.Info("creating anonymous gist")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Error("error uploading gist content", "err", err)
		return err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusCreated {
		e := fmt.Sprintf("unexpected HTTP status: %d", res.StatusCode)
		log.Error(e)
		return errors.New(e)
	}
	if err := json.NewDecoder(res.Body).Decode(g); err != nil {
		log.Error("error decoding HTTP response", "err", err)
		return err
	}
	return nil
}

func (g *Gist) CreateTarball(path string) error {
	tmpFile, err := os.Create(path)
	if err != nil {
		return err
	}
	defer tmpFile.Close()
	gz, err := gzip.NewWriterLevel(tmpFile, gzip.BestCompression)
	if err != nil {
		return err
	}
	defer gz.Close()
	tarball := tar.NewWriter(gz)
	for name, file := range g.Files {
		hdr := &tar.Header{
			Name:    name,
			Mode:    0644,
			ModTime: time.Now(),
			Size:    int64(len(file.Content)),
		}
		if err := tarball.WriteHeader(hdr); err != nil {
			return err
		}
		if _, err := tarball.Write([]byte(file.Content)); err != nil {
			return err
		}
	}
	if err := tarball.Close(); err != nil {
		return err
	}
	return nil
}

type File struct {
	Content string `json:"content"`
}
