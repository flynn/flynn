package client

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/flynn/flynn/deployer/types"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/discoverd/client/dialer"
	"github.com/flynn/flynn/pkg/httpclient"
	"github.com/flynn/flynn/pkg/sse"
)

// ErrNotFound is returned when no job was found.
var ErrNotFound = errors.New("deployer: not found")

type Client struct {
	*httpclient.Client
}

// New uses the default discoverd client and returns a client.
func New() (*Client, error) {
	if err := discoverd.Connect(""); err != nil {
		return nil, err
	}
	dialer := dialer.New(discoverd.DefaultClient, nil)
	c := &httpclient.Client{
		Dial:        dialer.Dial,
		DialClose:   dialer,
		ErrPrefix:   "deployer",
		ErrNotFound: ErrNotFound,
		URL:         "http://flynn-deployer",
	}
	c.HTTP = &http.Client{Transport: &http.Transport{Dial: c.Dial}}
	return &Client{Client: c}, nil
}

func (c *Client) CreateDeployment(deployment *deployer.Deployment) error {
	return c.Post("/deployments", deployment, deployment)
}

// DeploymentEventStream is a wrapper around an Events channel, allowing us to close
// the stream.
type DeploymentEventStream struct {
	Events chan *deployer.DeploymentEvent
	body   io.ReadCloser
}

// Close closes the underlying stream.
func (s *DeploymentEventStream) Close() {
	s.body.Close()
}

// StreamDeploymentEvents returns a DeploymentEventStream for an app.
func (c *Client) StreamDeploymentEvents(deploymentID string, lastID int64) (*DeploymentEventStream, error) {
	header := http.Header{
		"Accept":        []string{"text/event-stream"},
		"Last-Event-Id": []string{strconv.FormatInt(lastID, 10)},
	}
	res, err := c.RawReq("GET", fmt.Sprintf("/deployments/%s/events", deploymentID), header, nil, nil)
	if err != nil {
		return nil, err
	}
	stream := &DeploymentEventStream{Events: make(chan *deployer.DeploymentEvent), body: res.Body}
	go func() {
		defer close(stream.Events)
		dec := sse.NewDecoder(bufio.NewReader(stream.body))
		for {
			event := &deployer.DeploymentEvent{}
			if err := dec.Decode(event); err != nil {
				return
			}
			stream.Events <- event
		}
	}()
	return stream, nil
}
