package controller

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	ct "github.com/flynn/flynn-controller/types"
	"github.com/flynn/rpcplus"
)

func New(uri string) (*Client, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return nil, err
	}
	return &Client{url: uri, addr: u.Host}, nil
}

type Client struct {
	url  string
	addr string
}

var ErrNotFound = errors.New("controller: not found")

func (c *Client) send(method, path string, in, out interface{}) error {
	data, err := json.Marshal(in)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(method, c.url+path, bytes.NewBuffer(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return fmt.Errorf("controller: unexpected status %d", res.StatusCode)
	}
	if out != nil {
		return json.NewDecoder(res.Body).Decode(out)
	}
	return nil
}

func (c *Client) put(path string, in, out interface{}) error {
	return c.send("PUT", path, in, out)
}

func (c *Client) post(path string, in, out interface{}) error {
	return c.send("POST", path, in, out)
}

func (c *Client) get(path string, out interface{}) error {
	res, err := http.Get(c.url + path)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		if res.StatusCode == 404 {
			return ErrNotFound
		}
		return fmt.Errorf("controller: unexpected status %d", res.StatusCode)
	}
	return json.NewDecoder(res.Body).Decode(out)
}

func (c *Client) StreamFormations() (chan<- *ct.ExpandedFormation, *error) {
	// TODO: handle TLS
	client, err := rpcplus.DialHTTP("tcp", c.addr)
	if err != nil {
		return nil, &err
	}
	ch := make(chan *ct.ExpandedFormation)
	return ch, &client.StreamGo("Controller.StreamFormations", struct{}{}, ch).Error
}

func (c *Client) CreateArtifact(artifact *ct.Artifact) error {
	return c.post("/artifacts", artifact, artifact)
}

func (c *Client) CreateRelease(release *ct.Release) error {
	return c.post("/releases", release, release)
}

func (c *Client) SetAppRelease(appID, releaseID string) error {
	return c.put("/apps/"+appID+"/release", &ct.Release{ID: releaseID}, nil)
}

func (c *Client) GetAppRelease(appID string) (*ct.Release, error) {
	release := &ct.Release{}
	return release, c.get("/apps/"+appID+"/release", release)
}
