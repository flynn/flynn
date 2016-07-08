package testcluster

import (
	"crypto/tls"
	"errors"
	"net/http"
	"os"
	"time"

	"github.com/flynn/flynn/discoverd/client"
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

func (c *Client) AddHost(ch chan *discoverd.Event, vanilla bool) (*tc.Instance, error) {
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
		case e, ok := <-ch:
			if !ok {
				return nil, errors.New("channel closed unexpectedly")
			}
			if e.Kind != discoverd.EventKindUp {
				continue
			}
			c.size++
			return &instance, nil
		case <-time.After(60 * time.Second):
			return nil, errors.New("timed out waiting for new host")
		}
	}
}

func (c *Client) AddReleaseHosts() (*tc.BootResult, error) {
	var res tc.BootResult
	return &res, c.Post("/release", nil, &res)
}

func (c *Client) RemoveHost(ch chan *discoverd.Event, host *tc.Instance) error {
	if err := c.Delete("/" + host.ID); err != nil {
		return err
	}
	for {
		select {
		case e, ok := <-ch:
			if !ok {
				return errors.New("channel closed unexpectedly")
			}
			if e.Kind != discoverd.EventKindDown {
				continue
			}
			c.size--
			return nil
		case <-time.After(60 * time.Second):
			return errors.New("timed out waiting for host removal")
		}
	}
}
