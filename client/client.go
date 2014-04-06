package controller

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	ct "github.com/flynn/flynn-controller/types"
	"github.com/flynn/rpcplus"
	"github.com/flynn/strowger/types"
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

func (c *Client) StreamFormations(since *time.Time) (<-chan *ct.ExpandedFormation, *error) {
	if since == nil {
		*since = time.Unix(0, 0)
	}
	// TODO: handle TLS
	client, err := rpcplus.DialHTTP("tcp", c.addr)
	if err != nil {
		return nil, &err
	}
	ch := make(chan *ct.ExpandedFormation)
	return ch, &client.StreamGo("Controller.StreamFormations", since, ch).Error
}

func (c *Client) CreateArtifact(artifact *ct.Artifact) error {
	return c.post("/artifacts", artifact, artifact)
}

func (c *Client) CreateRelease(release *ct.Release) error {
	return c.post("/releases", release, release)
}

func (c *Client) CreateApp(app *ct.App) error {
	return c.post("/apps", app, app)
}

func (c *Client) CreateProvider(provider *ct.Provider) error {
	return c.post("/providers", provider, provider)
}

func (c *Client) ProvisionResource(req *ct.ResourceReq) (*ct.Resource, error) {
	if req.ProviderID == "" {
		return nil, errors.New("controller: missing provider id")
	}
	res := &ct.Resource{}
	err := c.post(fmt.Sprintf("/providers/%s/resources", req.ProviderID), req, res)
	return res, err
}

func (c *Client) PutResource(resource *ct.Resource) error {
	if resource.ID == "" || resource.ProviderID == "" {
		return errors.New("controller: missing id and/or provider id")
	}
	return c.put(fmt.Sprintf("/providers/%s/resources/%s", resource.ProviderID, resource.ID), resource, resource)
}

func (c *Client) PutFormation(formation *ct.Formation) error {
	if formation.AppID == "" || formation.ReleaseID == "" {
		return errors.New("controller: missing app id and/or release id")
	}
	return c.put(fmt.Sprintf("/apps/%s/formations/%s", formation.AppID, formation.ReleaseID), formation, formation)
}

func (c *Client) SetAppRelease(appID, releaseID string) error {
	return c.put(fmt.Sprintf("/apps/%s/release", appID), &ct.Release{ID: releaseID}, nil)
}

func (c *Client) GetAppRelease(appID string) (*ct.Release, error) {
	release := &ct.Release{}
	return release, c.get(fmt.Sprintf("/apps/%s/release", appID), release)
}

func (c *Client) CreateRoute(appID string, route *strowger.Route) error {
	return c.post(fmt.Sprintf("/apps/%s/routes", appID), route, route)
}
