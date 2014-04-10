package controller

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	ct "github.com/flynn/flynn-controller/types"
	"github.com/flynn/flynn-controller/utils"
	"github.com/flynn/go-discoverd"
	"github.com/flynn/go-discoverd/dialer"
	"github.com/flynn/rpcplus"
	"github.com/flynn/strowger/types"
)

func NewClient(uri string) (*Client, error) {
	if uri == "" {
		uri = "discoverd+http://flynn-controller"
	}
	u, err := url.Parse(uri)
	if err != nil {
		return nil, err
	}
	c := &Client{
		url:  uri,
		addr: u.Host,
		http: http.DefaultClient,
	}
	if u.Scheme == "discoverd+http" {
		if err := discoverd.Connect(""); err != nil {
			return nil, err
		}
		c.dialer = dialer.New(discoverd.DefaultClient, nil)
		c.http = &http.Client{Transport: &http.Transport{Dial: c.dialer.Dial}}
		u.Scheme = "http"
		c.url = u.String()
	}
	return c, nil
}

type Client struct {
	url  string
	addr string
	http *http.Client

	dialer dialer.Dialer
}

func (c *Client) Close() error {
	return c.dialer.Close()
}

var ErrNotFound = errors.New("controller: not found")

func toJSON(v interface{}) (io.Reader, error) {
	data, err := json.Marshal(v)
	return bytes.NewBuffer(data), err
}

func (c *Client) rawReq(method, path string, contentType string, in, out interface{}) (*http.Response, error) {
	var payload io.Reader
	switch v := in.(type) {
	case io.Reader:
		payload = v
	case nil:
	default:
		var err error
		payload, err = toJSON(in)
		if err != nil {
			return nil, err
		}
	}

	req, err := http.NewRequest(method, c.url+path, payload)
	if err != nil {
		return nil, err
	}
	if contentType == "" {
		contentType = "application/json"
	}
	req.Header.Set("Content-Type", contentType)
	res, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode == 404 {
		res.Body.Close()
		return res, ErrNotFound
	}
	if res.StatusCode != 200 {
		res.Body.Close()
		return res, &url.Error{
			Op:  req.Method,
			URL: req.URL.String(),
			Err: fmt.Errorf("controller: unexpected status %d", res.StatusCode),
		}
	}
	if out != nil {
		defer res.Body.Close()
		return res, json.NewDecoder(res.Body).Decode(out)
	}
	return res, nil
}

func (c *Client) send(method, path string, in, out interface{}) error {
	_, err := c.rawReq(method, path, "", in, out)
	return err
}

func (c *Client) put(path string, in, out interface{}) error {
	return c.send("PUT", path, in, out)
}

func (c *Client) post(path string, in, out interface{}) error {
	return c.send("POST", path, in, out)
}

func (c *Client) get(path string, out interface{}) error {
	_, err := c.rawReq("GET", path, "", nil, out)
	return err
}

func (c *Client) StreamFormations(since *time.Time) (<-chan *ct.ExpandedFormation, *error) {
	if since == nil {
		s := time.Unix(0, 0)
		since = &s
	}
	// TODO: handle TLS
	var dial rpcplus.DialFunc
	if c.dialer != nil {
		dial = c.dialer.Dial
	}
	client, err := rpcplus.DialHTTPPath("tcp", c.addr, rpcplus.DefaultRPCPath, dial)
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

func (c *Client) GetFormation(appID, releaseID string) (*ct.Formation, error) {
	formation := &ct.Formation{}
	return formation, c.get(fmt.Sprintf("/apps/%s/formations/%s", appID, releaseID), formation)
}

func (c *Client) GetRelease(releaseID string) (*ct.Release, error) {
	release := &ct.Release{}
	return release, c.get(fmt.Sprintf("/releases/%s", releaseID), release)
}

func (c *Client) GetArtifact(artifactID string) (*ct.Artifact, error) {
	artifact := &ct.Artifact{}
	return artifact, c.get(fmt.Sprintf("/artifacts/%s", artifactID), artifact)
}

func (c *Client) GetApp(appID string) (*ct.App, error) {
	app := &ct.App{}
	return app, c.get(fmt.Sprintf("/apps/%s", appID), app)
}

func (c *Client) GetJobLog(appID, jobID string) (io.ReadCloser, error) {
	res, err := c.rawReq("GET", fmt.Sprintf("/apps/%s/jobs/%s/log", appID, jobID), "", nil, nil)
	if err != nil {
		return nil, err
	}
	return res.Body, nil
}

func (c *Client) RunJobAttached(appID string, job *ct.NewJob) (utils.ReadWriteCloser, error) {
	data, err := toJSON(job)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest("POST", fmt.Sprintf("%s/apps/%s/jobs", c.url, appID), data)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/vnd.flynn.attach")
	var dial dialer.DialFunc
	if c.dialer != nil {
		dial = c.dialer.Dial
	}
	res, rwc, err := utils.HijackRequest(req, dial)
	if err != nil {
		res.Body.Close()
		return nil, err
	}
	return rwc, nil
}

func (c *Client) RunJobDetached(appID string, req *ct.NewJob) (*ct.Job, error) {
	job := &ct.Job{}
	return job, c.post(fmt.Sprintf("/apps/%s/jobs", appID), req, job)
}

func (c *Client) GetJobList(appID string) ([]*ct.Job, error) {
	var jobs []*ct.Job
	return jobs, c.get(fmt.Sprintf("/apps/%s/jobs", appID), &jobs)
}
