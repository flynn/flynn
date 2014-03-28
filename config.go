package main

import (
	"encoding/json"
	"io"
	"os"

	"github.com/flynn/flynn-host/types"
)

func openConfig(file string) (*host.Host, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return parseConfig(f)
}

func parseConfig(r io.Reader) (*host.Host, error) {
	var conf Config
	if err := json.NewDecoder(r).Decode(&conf); err != nil {
		return nil, err
	}
	return conf.hostConfig()
}

type Config struct {
	Attributes map[string]string `json:"attributes"`
}

func (c *Config) hostConfig() (*host.Host, error) {
	h := &host.Host{Attributes: c.Attributes}
	return h, nil
}
