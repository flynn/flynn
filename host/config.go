package main

import (
	"encoding/json"
	"io"
	"os"

	"github.com/flynn/flynn/host/types"
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
	Metadata map[string]string `json:"metadata"`
}

func (c *Config) hostConfig() (*host.Host, error) {
	h := &host.Host{Metadata: c.Metadata}
	return h, nil
}
