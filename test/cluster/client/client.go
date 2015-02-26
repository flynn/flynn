package testcluster

import (
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/flynn/flynn/host/types"
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

func (c *Client) AddHost(ch chan *host.HostEvent, vanilla bool) (*tc.Instance, error) {
	var instance tc.Instance
	path := ""
	if vanilla {
		path = "?vanilla=true"
	}
	if err := c.Post(path, nil, &instance); err != nil {
		return nil, err
	}
	c.cluster.Instances = append(c.cluster.Instances, &instance)
	if vanilla {
		return &instance, nil
	}
	for {
		select {
		case event := <-ch:
			if event.HostID == instance.ID {
				c.size++
				return &instance, nil
			}
		case <-time.After(60 * time.Second):
			return nil, fmt.Errorf("timed out waiting for new host")
		}
	}
}

func (c *Client) RemoveHost(host *tc.Instance) error {
	c.size--
	return c.Delete("/" + host.ID)
}

func (c *Client) DumpLogs(out io.Writer) error {
	res, err := c.RawReq("GET", "/dump-logs", nil, nil, nil)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, res.Body)
	return err
}
