package testcluster

import (
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/pkg/httpclient"
	tc "github.com/flynn/flynn/test/cluster"
)

type Client struct {
	*httpclient.Client
	cluster *tc.Cluster
}

var ErrNotFound = errors.New("testcluster: resource not found")

func NewClient(endpoint string) (*Client, error) {
	httpClient := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{ServerName: "ci.flynn.io"}}}
	client := &Client{
		Client: &httpclient.Client{
			ErrNotFound: ErrNotFound,
			URL:         endpoint,
			HTTP:        httpClient,
		},
	}
	var cluster tc.Cluster
	if err := client.Get("", &cluster); err != nil {
		return nil, err
	}
	client.cluster = &cluster
	return client, nil
}

func (c *Client) Size() int {
	return c.cluster.Size()
}

func (c *Client) BackoffPeriod() time.Duration {
	return c.cluster.BackoffPeriod
}

func (c *Client) AddHost(ch chan *host.HostEvent) (string, error) {
	var instance tc.Instance
	if err := c.Post("", nil, &instance); err != nil {
		return "", err
	}
	c.cluster.Instances = append(c.cluster.Instances, &instance)
	for {
		select {
		case event := <-ch:
			if event.HostID == instance.ID {
				return instance.ID, nil
			}
		case <-time.After(60 * time.Second):
			return "", fmt.Errorf("timed out waiting for new host")
		}
	}
}

func (c *Client) RemoveHost(id string) error {
	return c.Delete("/" + id)
}

func (c *Client) DumpLogs(out io.Writer) error {
	res, err := c.RawReq("GET", "/dump-logs", nil, nil, nil)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, res.Body)
	return err
}
