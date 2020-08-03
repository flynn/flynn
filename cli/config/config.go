package config

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"

	"github.com/BurntSushi/toml"
	"github.com/flynn/flynn/cli/login/tokensource"
	controller "github.com/flynn/flynn/controller/client"
	tarclient "github.com/flynn/flynn/tarreceive/client"
	"github.com/mitchellh/go-homedir"
	"golang.org/x/oauth2"
)

var ErrNoDockerPushURL = errors.New("ERROR: Docker push URL not configured, set it with 'flynn docker set-push-url'")

type Cluster struct {
	Name          string `json:"name"`
	OAuthURL      string `json:"oauth_url,omitempty" toml:"OAuthURL,omitempty"`
	Key           string `json:"key,omitempty" toml:"Key,omitempty"`
	TLSPin        string `json:"tls_pin" toml:"TLSPin,omitempty"`
	ControllerURL string `json:"controller_url"`
	GitURL        string `json:"git_url"`
	ImageURL      string `json:"image_url"`
	DockerPushURL string `json:"docker_push_url,omitempty" toml:"DockerPushURL,omitempty"`
}

func (c *Cluster) Client() (controller.Client, error) {
	if c.OAuthURL != "" {
		ts, err := tokensource.New(c.OAuthURL, c.ControllerURL, TokenCache())
		if err != nil {
			return nil, err
		}
		return controller.NewClientWithHTTP(c.ControllerURL, "", oauth2.NewClient(context.Background(), ts))
	}

	var pin []byte
	if c.TLSPin != "" {
		var err error
		pin, err = base64.StdEncoding.DecodeString(c.TLSPin)
		if err != nil {
			return nil, fmt.Errorf("error decoding tls pin: %s", err)
		}
	}
	return controller.NewClientWithConfig(c.ControllerURL, c.Key, controller.Config{Pin: pin})
}

func (c *Cluster) TarClient() (*tarclient.Client, error) {
	if c.ImageURL == "" {
		return nil, errors.New("cluster: missing ImageURL .flynnrc config")
	}
	if c.OAuthURL != "" {
		ts, err := tokensource.New(c.OAuthURL, c.ControllerURL, TokenCache())
		if err != nil {
			return nil, err
		}
		return tarclient.NewClientWithHTTP(c.ImageURL, oauth2.NewClient(context.Background(), ts)), nil
	}

	var pin []byte
	if c.TLSPin != "" {
		var err error
		pin, err = base64.StdEncoding.DecodeString(c.TLSPin)
		if err != nil {
			return nil, fmt.Errorf("error decoding tls pin: %s", err)
		}
	}
	return tarclient.NewClientWithConfig(c.ImageURL, c.Key, tarclient.Config{Pin: pin}), nil
}

func (c *Cluster) DockerPushHost() (string, error) {
	if c.DockerPushURL == "" {
		return "", ErrNoDockerPushURL
	}
	u, err := url.Parse(c.DockerPushURL)
	if err != nil {
		return "", fmt.Errorf("cluster: could not parse DockerPushURL: %s", err)
	}
	return u.Host, nil
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

func TokenCache() tokensource.Cache {
	return tokensource.NewTokenCache(filepath.Join(Dir(), "tokens"))
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
	var msg string
	conflictIdx := -1
	for i, existing := range c.Clusters {
		var m string
		switch {
		case existing.Name == s.Name:
			m = fmt.Sprintf("Cluster %q already exists in ~/.flynnrc", s.Name)
		case existing.GitURL != "" && existing.GitURL == s.GitURL:
			m = fmt.Sprintf("A cluster with the URL %q already exists in ~/.flynnrc", s.GitURL)
		case existing.ControllerURL == s.ControllerURL:
			m = fmt.Sprintf("A cluster with the URL %q already exists in ~/.flynnrc", s.ControllerURL)
		case existing.DockerPushURL != "" && existing.DockerPushURL == s.DockerPushURL:
			m = fmt.Sprintf("A cluster with the URL %q already exists in ~/.flynnrc", s.DockerPushURL)
		}
		if m != "" {
			if conflictIdx != -1 && conflictIdx != i {
				return fmt.Errorf("The cluster name and/or URLs conflict with multiple existing clusters.")
			}
			conflictIdx = i
			msg = m
		}
	}

	// The new cluster config conflicts with an existing one
	if msg != "" {
		if !force {
			return fmt.Errorf(msg)
		}

		// Remove conflicting cluster
		c.Clusters = append(c.Clusters[:conflictIdx], c.Clusters[conflictIdx+1:]...)
	}

	c.Clusters = append(c.Clusters, s)

	return nil
}

func (c *Config) Upgrade() (changed bool) {
	// Any "config migrations" should be done in this function
	return false
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
