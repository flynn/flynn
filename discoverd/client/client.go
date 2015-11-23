package discoverd

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/flynn/flynn/pkg/httpclient"
	"github.com/flynn/flynn/pkg/stream"
)

var ErrTimedOut = errors.New("discoverd: timed out waiting for instances")

type Config struct {
	Endpoints []string
}

type Client struct {
	servers []*httpclient.Client
	pinned  int
	leader  int
	mu      sync.RWMutex
}

func NewClientWithConfig(config Config) *Client {
	client := Client{}
	for _, e := range config.Endpoints {
		client.servers = append(client.servers, client.httpClient(e))
	}
	return &client
}

func NewClientWithURL(url string) *Client {
	if !strings.HasPrefix(url, "http") {
		url = "http://" + url
	}
	return NewClientWithConfig(Config{Endpoints: []string{url}})
}

func NewClient() *Client {
	return NewClientWithConfig(defaultConfig())
}

func defaultConfig() Config {
	urls := os.Getenv("DISCOVERD")
	if urls == "" {
		urls = "http://127.0.0.1:1111"
	}
	return Config{Endpoints: strings.Split(urls, ",")}
}

func (c *Client) updateLeader(url string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, s := range c.servers {
		if s.Host == url {
			c.leader = i
		}
	}
}

func (c *Client) httpClient(url string) *httpclient.Client {
	checkRedirect := func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return fmt.Errorf("too many redirects")
		}
		if len(via) > 0 {
			for attr, val := range via[0].Header {
				if _, ok := req.Header[attr]; !ok {
					req.Header[attr] = val
				}
			}
		}
		c.updateLeader(req.Host)
		return nil
	}
	return &httpclient.Client{
		URL: url,
		HTTP: &http.Client{
			CheckRedirect: checkRedirect,
		},
	}
}

func (c *Client) Send(method string, path string, in, out interface{}) error {
	// Cluster logic goes here
	return nil
}

func (s *Client) Stream(method string, path string, in, out interface{}) (stream.Stream, error) {
	return nil, nil
}

func (c *Client) Get(path string, out interface{}) error {
	return c.Send("GET", path, nil, out)
}

func (c *Client) Put(path string, in, out interface{}) error {
	return c.Send("PUT", path, in, out)
}

func (c *Client) Delete(path string) error {
	return c.Send("DELETE", path, nil, nil)
}

func (c *Client) Ping(url string) error {
	// find appropriate server and send ping
	return nil
}
