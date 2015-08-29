package config

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"runtime"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/BurntSushi/toml"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/mitchellh/go-homedir"
	"github.com/flynn/flynn/controller/client"
)

type Cluster struct {
	Name   string `json:"name"`
	Domain string `json:"domain"`
	Key    string `json:"key"`
	TLSPin string `json:"tls_pin"`

	// GitHost and URL are legacy config options for clusters that are using git
	// over SSH, they should be removed at some point in the near future.
	GitHost string `json:"git_host"`
	URL     string `json:"url"`
}

func (c *Cluster) Client() (*controller.Client, error) {
	url := c.URL
	if url == "" {
		url = "https://controller." + c.Domain
	}

	var client *controller.Client
	var err error
	if c.TLSPin != "" {
		pin, err := base64.StdEncoding.DecodeString(c.TLSPin)
		if err != nil {
			return nil, fmt.Errorf("error decoding tls pin: %s", err)
		}
		client, err = controller.NewClientWithConfig(url, c.Key, controller.Config{Pin: pin})
	} else {
		client, err = controller.NewClient(url, c.Key)
	}

	return client, err
}

type Config struct {
	Default  string     `toml:"default"`
	Clusters []*Cluster `toml:"cluster"`
}

func HomeDir() string {
	dir, err := homedir.Dir()
	if err != nil {
		panic(err)
	}
	return dir
}

func Dir() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(os.Getenv("APPDATA"), "flynn")
	}
	return filepath.Join(HomeDir(), ".flynn")
}

func DefaultPath() string {
	if p := os.Getenv("FLYNNRC"); p != "" {
		return p
	}
	if runtime.GOOS == "windows" {
		return filepath.Join(Dir(), "flynnrc")
	}
	return filepath.Join(HomeDir(), ".flynnrc")
}

func ReadFile(path string) (*Config, error) {
	c := &Config{}
	_, err := toml.DecodeFile(path, c)
	if err != nil {
		return c, err
	}
	return c, nil
}

func (c *Config) Marshal() []byte {
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(c); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func (c *Config) Add(s *Cluster, force bool) error {
	if s.Domain == "" && s.GitHost == "" {
		u, err := url.Parse(s.URL)
		if err != nil {
			return err
		}
		if host, _, err := net.SplitHostPort(u.Host); err == nil {
			s.GitHost = host
		} else {
			s.GitHost = u.Host
		}
	}

	for i, existing := range c.Clusters {
		msg := ""

		switch {
		case existing.Name == s.Name:
			msg = fmt.Sprintf("Cluster %q already exists in ~/.flynnrc", s.Name)
		case existing.URL == s.URL:
			msg = fmt.Sprintf("A cluster with the URL %q already exists in ~/.flynnrc", s.URL)
		case existing.GitHost == s.GitHost:
			msg = fmt.Sprintf("A cluster with the git host %q already exists in ~/.flynnrc", s.GitHost)
		case s.Domain != "" && existing.Domain == s.Domain:
			msg = fmt.Sprintf("A cluster with the domain %q already exists in ~/.flynnrc", s.Domain)
		}

		// The new cluster config match with existing one
		if msg != "" {
			if !force {
				return fmt.Errorf(msg)
			}

			// Remove existing match
			c.Clusters = append(c.Clusters[:i], c.Clusters[i+1:]...)
		}
	}

	// Did not match
	c.Clusters = append(c.Clusters, s)

	return nil
}

func (c *Config) Remove(name string) *Cluster {
	for i, s := range c.Clusters {
		if s.Name != name {
			continue
		}
		c.Clusters = append(c.Clusters[:i], c.Clusters[i+1:]...)
		return s
	}
	return nil
}

func (c *Config) SetDefault(name string) bool {
	for _, s := range c.Clusters {
		if s.Name != name {
			continue
		}
		c.Default = name
		return true
	}
	return false
}

func (c *Config) SaveTo(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	if len(c.Clusters) != 0 {
		if err := toml.NewEncoder(f).Encode(c); err != nil {
			return err
		}
		f.Write([]byte("\n"))
	}
	return nil
}
