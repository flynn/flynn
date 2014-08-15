package config

import (
	"bytes"
	"fmt"
	"net"
	"net/url"
	"os"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/BurntSushi/toml"
)

type Server struct {
	Name    string `json:"name"`
	GitHost string `json:"git_host"`
	URL     string `json:"url"`
	Key     string `json:"key"`
	TLSPin  string `json:"tls_pin"`
}

type Config struct {
	Servers []*Server `toml:"server"`
}

func (c *Config) Marshal() []byte {
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(c); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func (c *Config) Add(s *Server) error {
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

	for _, existing := range c.Servers {
		if existing.Name == s.Name {
			return fmt.Errorf("Server %q already exists in ~/.flynnrc", s.Name)
		}
		if existing.URL == s.URL {
			return fmt.Errorf("A server with the URL %q already exists in ~/.flynnrc", s.URL)
		}
		if existing.GitHost == s.GitHost {
			return fmt.Errorf("A server with the git host %q already exists in ~/.flynnrc", s.GitHost)
		}
	}

	c.Servers = append(c.Servers, s)
	return nil
}

func (c *Config) Remove(name string) bool {
	for i, s := range c.Servers {
		if s.Name != name {
			continue
		}
		c.Servers = append(c.Servers[:i], c.Servers[i+1:]...)
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

	if len(c.Servers) != 0 {
		if err := toml.NewEncoder(f).Encode(c); err != nil {
			return err
		}
	}
	return nil
}
