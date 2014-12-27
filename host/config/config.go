package config

import (
	"encoding/json"
	"io"
	"io/ioutil"
	"os"
	"strings"
)

func Open(file string) (*Config, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Parse(f)
}

func Parse(r io.Reader) (*Config, error) {
	conf := &Config{}
	if err := json.NewDecoder(r).Decode(conf); err != nil {
		return nil, err
	}
	return conf, nil
}

type Config struct {
	Args []string          `json:"args,omitempty"`
	Env  map[string]string `json:"env,omitempty"`
}

func New() *Config {
	return &Config{Env: make(map[string]string)}
}

func (c *Config) DeleteArgs(names ...string) {
	for _, n := range names {
		for i, arg := range c.Args {
			if strings.HasPrefix(arg, "--"+n) {
				c.Args = append(c.Args[:i], c.Args[i+1:]...)
			}
		}
	}
}

func (c *Config) DeleteEnvs(names ...string) {
	for _, n := range names {
		delete(c.Env, n)
	}
}

func (c *Config) WriteTo(name string) error {
	data, err := json.MarshalIndent(c, "", "\t")
	if err != nil {
		return err
	}
	return ioutil.WriteFile(name, append(data, '\n'), 0644)
}
