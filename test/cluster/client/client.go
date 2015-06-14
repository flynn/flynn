package testcluster

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/flynn/flynn/pkg/cluster"
	"github.com/flynn/flynn/pkg/httpclient"
	tc "github.com/flynn/flynn/test/cluster"
)

type Client struct {
	*httpclient.Client
	cluster *tc.Cluster
	size    int
}

var ErrNotFound = errors.New("testcluster: resource not found")

func NewClient(endpoint string) (*Client, error) {
	authKey := os.Getenv("TEST_RUNNER_AUTH_KEY")
	if authKey == "" {
		return nil, errors.New("missing TEST_RUNNER_AUTH_KEY environment variable")
	}

	httpClient := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{ServerName: "ci.flynn.io"}}}
	client := &Client{
		Client: &httpclient.Client{
			ErrNotFound: ErrNotFound,
			URL:         endpoint,
			Key:         authKey,
			HTTP:        httpClient,
		},
	}
	var cluster tc.Cluster
	if err := client.Get("", &cluster); err != nil {
		return nil, err
	}
	client.cluster = &cluster
	client.size = cluster.Size()
	return client, nil
}

func (c *Client) Size() int {
	return c.size
}

func (c *Client) BackoffPeriod() time.Duration {
	return c.cluster.BackoffPeriod
}

func (c *Client) AddHost(ch chan *cluster.Host, vanilla bool) (*tc.Instance, error) {
	path := ""
	if vanilla {
		path = "?vanilla=true"
	}
	var instance tc.Instance
	if err := c.Post(path, nil, &instance); err != nil {
		return nil, err
	}
	c.cluster.Instances = append(c.cluster.Instances, &instance)
	if vanilla {
		return &instance, nil
	}
	for {
		select {
		case h := <-ch:
			if h.ID() == instance.ID {
				c.size++
				return &instance, nil
			}
		case <-time.After(60 * time.Second):
			return nil, fmt.Errorf("timed out waiting for new host")
		}
	}
}

func (c *Client) AddReleaseHosts() (*tc.BootResult, error) {
	var res tc.BootResult
	return &res, c.Post("/release", nil, &res)
}

func (c *Client) RemoveHost(host *tc.Instance) error {
	c.size--
	return c.Delete("/" + host.ID)
}
