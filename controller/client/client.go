package controller

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/controller/utils"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/discoverd/client/dialer"
	"github.com/flynn/flynn/pkg/pinned"
	"github.com/flynn/flynn/pkg/rpcplus"
	"github.com/flynn/flynn/router/types"
)

func NewClient(uri, key string) (*Client, error) {
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
		key:  key,
	}
	if u.Scheme == "discoverd+http" {
		if err := discoverd.Connect(""); err != nil {
			return nil, err
		}
		dialer := dialer.New(discoverd.DefaultClient, nil)
		c.dial = dialer.Dial
		c.dialClose = dialer
		c.http = &http.Client{Transport: &http.Transport{Dial: c.dial}}
		u.Scheme = "http"
		c.url = u.String()
	}
	return c, nil
}

func NewClientWithPin(uri, key string, pin []byte) (*Client, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return nil, err
	}
	c := &Client{
		dial: (&pinned.Config{Pin: pin}).Dial,
		key:  key,
	}
	if _, port, _ := net.SplitHostPort(u.Host); port == "" {
		u.Host += ":443"
	}
	c.addr = u.Host
	u.Scheme = "http"
	c.url = u.String()
	c.http = &http.Client{Transport: &http.Transport{Dial: c.dial}}
	return c, nil
}

type Client struct {
	url  string
	key  string
	addr string
	http *http.Client

	dial      rpcplus.DialFunc
	dialClose io.Closer
}

func (c *Client) Close() error {
	if c.dialClose != nil {
		c.dialClose.Close()
	}
	return nil
}

var ErrNotFound = errors.New("controller: not found")

func toJSON(v interface{}) (io.Reader, error) {
	data, err := json.Marshal(v)
	return bytes.NewBuffer(data), err
}

func (c *Client) rawReq(method, path string, header http.Header, in, out interface{}) (*http.Response, error) {
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
	if header == nil {
		header = make(http.Header)
	}
	if header.Get("Content-Type") == "" {
		header.Set("Content-Type", "application/json")
	}
	req.Header = header
	req.SetBasicAuth("", c.key)
	res, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode == 404 {
		res.Body.Close()
		return res, ErrNotFound
	}
	if res.StatusCode == 400 {
		var body ct.ValidationError
		defer res.Body.Close()
		if err = json.NewDecoder(res.Body).Decode(&body); err != nil {
			return res, err
		}
		return res, body
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
	_, err := c.rawReq(method, path, nil, in, out)
	return err
}

func (c *Client) put(path string, in, out interface{}) error {
	return c.send("PUT", path, in, out)
}

func (c *Client) post(path string, in, out interface{}) error {
	return c.send("POST", path, in, out)
}

func (c *Client) get(path string, out interface{}) error {
	_, err := c.rawReq("GET", path, nil, nil, out)
	return err
}

func (c *Client) delete(path string) error {
	res, err := c.rawReq("DELETE", path, nil, nil, nil)
	if err == nil {
		res.Body.Close()
	}
	return err
}

type FormationUpdates struct {
	Chan <-chan *ct.ExpandedFormation

	conn net.Conn
}

func (u *FormationUpdates) Close() error {
	if u.conn == nil {
		return nil
	}
	return u.conn.Close()
}

func (c *Client) StreamFormations(since *time.Time) (*FormationUpdates, *error) {
	if since == nil {
		s := time.Unix(0, 0)
		since = &s
	}
	dial := c.dial
	if dial == nil {
		dial = net.Dial
	}
	ch := make(chan *ct.ExpandedFormation)
	conn, err := dial("tcp", c.addr)
	if err != nil {
		close(ch)
		return &FormationUpdates{ch, conn}, &err
	}
	header := make(http.Header)
	header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(":"+c.key)))
	client, err := rpcplus.NewHTTPClient(conn, rpcplus.DefaultRPCPath, header)
	if err != nil {
		close(ch)
		return &FormationUpdates{ch, conn}, &err
	}
	return &FormationUpdates{ch, conn}, &client.StreamGo("Controller.StreamFormations", since, ch).Error
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

func (c *Client) PutJob(job *ct.Job) error {
	if job.ID == "" || job.AppID == "" {
		return errors.New("controller: missing job id and/or app id")
	}
	return c.put(fmt.Sprintf("/apps/%s/jobs/%s", job.AppID, job.ID), job, job)
}

func (c *Client) DeleteJob(appID, jobID string) error {
	return c.delete(fmt.Sprintf("/apps/%s/jobs/%s", appID, jobID))
}

func (c *Client) SetAppRelease(appID, releaseID string) error {
	return c.put(fmt.Sprintf("/apps/%s/release", appID), &ct.Release{ID: releaseID}, nil)
}

func (c *Client) GetAppRelease(appID string) (*ct.Release, error) {
	release := &ct.Release{}
	return release, c.get(fmt.Sprintf("/apps/%s/release", appID), release)
}

func (c *Client) RouteList(appID string) ([]*router.Route, error) {
	var routes []*router.Route
	return routes, c.get(fmt.Sprintf("/apps/%s/routes", appID), &routes)
}

func (c *Client) CreateRoute(appID string, route *router.Route) error {
	return c.post(fmt.Sprintf("/apps/%s/routes", appID), route, route)
}

func (c *Client) DeleteRoute(appID string, routeID string) error {
	return c.delete(fmt.Sprintf("/apps/%s/routes/%s", appID, routeID))
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

type sseDecoder struct {
	*bufio.Reader
}

// Decode finds the next "data" field and decodes it into v
func (dec *sseDecoder) Decode(v interface{}) error {
	for {
		line, err := dec.ReadBytes('\n')
		if err != nil {
			return err
		}
		if bytes.HasPrefix(line, []byte("data: ")) {
			data := bytes.TrimPrefix(line, []byte("data: "))
			return json.Unmarshal(data, v)
		}
	}
}

type JobEventStream struct {
	Events chan *ct.JobEvent
	body   io.ReadCloser
}

func (s *JobEventStream) Close() {
	s.body.Close()
}

func (c *Client) StreamJobEvents(appID string) (*JobEventStream, error) {
	res, err := c.rawReq("GET", fmt.Sprintf("/apps/%s/jobs", appID), http.Header{"Accept": []string{"text/event-stream"}}, nil, nil)
	if err != nil {
		return nil, err
	}
	stream := &JobEventStream{Events: make(chan *ct.JobEvent), body: res.Body}
	go func() {
		defer close(stream.Events)
		dec := &sseDecoder{bufio.NewReader(stream.body)}
		for {
			event := &ct.JobEvent{}
			if err := dec.Decode(event); err != nil {
				return
			}
			stream.Events <- event
		}
	}()
	return stream, nil
}

func (c *Client) GetJobLog(appID, jobID string, tail bool) (io.ReadCloser, error) {
	path := fmt.Sprintf("/apps/%s/jobs/%s/log", appID, jobID)
	if tail {
		path += "?tail=true"
	}
	res, err := c.rawReq("GET", path, nil, nil, nil)
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
	req.SetBasicAuth("", c.key)
	var dial rpcplus.DialFunc
	if c.dial != nil {
		dial = c.dial
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

func (c *Client) JobList(appID string) ([]*ct.Job, error) {
	var jobs []*ct.Job
	return jobs, c.get(fmt.Sprintf("/apps/%s/jobs", appID), &jobs)
}

func (c *Client) AppList() ([]*ct.App, error) {
	var apps []*ct.App
	return apps, c.get("/apps", &apps)
}

func (c *Client) KeyList() ([]*ct.Key, error) {
	var keys []*ct.Key
	return keys, c.get("/keys", &keys)
}

func (c *Client) CreateKey(pubKey string) (*ct.Key, error) {
	key := &ct.Key{}
	return key, c.post("/keys", &ct.Key{Key: pubKey}, key)
}

func (c *Client) DeleteKey(id string) error {
	return c.delete("/keys/" + strings.Replace(id, ":", "", -1))
}

func (c *Client) ProviderList() ([]*ct.Provider, error) {
	var providers []*ct.Provider
	return providers, c.get("/providers", &providers)
}
