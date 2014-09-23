package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
)

type Gist struct {
	URL         string          `json:"html_url,omitempty"`
	Description string          `json:"description"`
	Public      bool            `json:"public"`
	Files       map[string]File `json:"files"`
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
}

func (g *Gist) Upload() error {
	if len(g.Files) == 0 {
		return errors.New("cannot create empty gist")
	}

	payload, err := json.Marshal(g)
	if err != nil {
		return err
	}
	req, err := http.NewRequest("POST", "https://api.github.com/gists", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusCreated {
		return fmt.Errorf("expected %d status, got %d", http.StatusCreated, res.StatusCode)
	}
	if err := json.NewDecoder(res.Body).Decode(g); err != nil {
		return err
	}
	return nil
}

type File struct {
	Content string `json:"content"`
}
