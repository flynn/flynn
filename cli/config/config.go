package config

import (
	"bytes"
	"fmt"
	"net"
	"net/url"
	"os"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/BurntSushi/toml"
)

type Cluster struct {
	Name    string `json:"name"`
	GitHost string `json:"git_host"`
	URL     string `json:"url"`
	Key     string `json:"key"`
	TLSPin  string `json:"tls_pin"`
}

type Config struct {
	Default  string     `toml:"default"`
	Clusters []*Cluster `toml:"cluster"`
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
	if s.GitHost == "" {
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

func (c *Config) Remove(name string) bool {
	for i, s := range c.Clusters {
		if s.Name != name {
			continue
		}
		c.Clusters = append(c.Clusters[:i], c.Clusters[i+1:]...)
		return true
	}
	return false
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
