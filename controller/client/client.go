// Package controller provides a client for the controller API.
package controller

import (
	"bufio"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/controller/utils"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/discoverd/client/dialer"
	"github.com/flynn/flynn/pkg/httpclient"
	"github.com/flynn/flynn/pkg/pinned"
	"github.com/flynn/flynn/pkg/rpcplus"
	"github.com/flynn/flynn/pkg/sse"
	"github.com/flynn/flynn/router/types"
)

// Client is a client for the controller API.
type Client struct {
	addr string
	*httpclient.Client
}

// ErrNotFound is returned when a resource is not found (HTTP status 404).
var ErrNotFound = errors.New("controller: resource not found")

// newClient creates a generic Client object, additional attributes must
// be set by the caller
func newClient(addr string, key string, url string, http *http.Client) *Client {
	c := &Client{
		addr: addr,
		Client: &httpclient.Client{
			ErrPrefix:   "controller",
			ErrNotFound: ErrNotFound,
			Key:         key,
			URL:         url,
			HTTP:        http,
		},
	}
	return c
}

func newDiscoverdClient(u *url.URL, key string) (*Client, error) {
	if err := discoverd.Connect(""); err != nil {
		return nil, err
	}
	u.Scheme = "http"
	d := dialer.New(discoverd.DefaultClient, nil)
	httpClient := &http.Client{Transport: &http.Transport{Dial: d.Dial}}
	c := newClient(u.Host, key, u.String(), httpClient)
	c.Dial = d.Dial
	c.DialClose = d
	return c, nil
}

// NewClient creates a new Client pointing at uri and using key for
// authentication.
func NewClient(uri, key string) (*Client, error) {
	if uri == "" {
		uri = "discoverd+http://flynn-controller"
	}
	u, err := url.Parse(uri)
	if err != nil {
		return nil, err
	}
	if u.Scheme == "discoverd+http" {
		return newDiscoverdClient(u, key)
	}
	return newClient(u.Host, key, u.String(), http.DefaultClient), nil
}

// NewClientWithPin acts like NewClient, but specifies a TLS pin.
func NewClientWithPin(uri, key string, pin []byte) (*Client, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return nil, err
	}
	if _, port, _ := net.SplitHostPort(u.Host); port == "" {
		u.Host += ":443"
	}
	u.Scheme = "http"
	d := &pinned.Config{Pin: pin}
	httpClient := &http.Client{Transport: &http.Transport{Dial: d.Dial}}
	c := newClient(u.Host, key, u.String(), httpClient)
	c.Dial = d.Dial
	return c, nil
}

// FormationUpdates is a wrapper around a Chan channel, allowing us to close
// the stream.
type FormationUpdates struct {
	Chan <-chan *ct.ExpandedFormation

	conn net.Conn
}

// Close closes the underlying stream.
func (u *FormationUpdates) Close() error {
	if u.conn == nil {
		return nil
	}
	return u.conn.Close()
}

// StreamFormations returns a FormationUpdates stream. If since is not nil, only
// retrieves formation updates since the specified time.
func (c *Client) StreamFormations(since *time.Time) (*FormationUpdates, *error) {
	if since == nil {
		s := time.Unix(0, 0)
		since = &s
	}
	dial := c.Dial
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
	header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(":"+c.Key)))
	client, err := rpcplus.NewHTTPClient(conn, rpcplus.DefaultRPCPath, header)
	if err != nil {
		close(ch)
		return &FormationUpdates{ch, conn}, &err
	}
	return &FormationUpdates{ch, conn}, &client.StreamGo("Controller.StreamFormations", since, ch).Error
}

// CreateArtifact creates a new artifact.
func (c *Client) CreateArtifact(artifact *ct.Artifact) error {
	return c.Post("/artifacts", artifact, artifact)
}

// CreateRelease creates a new release.
func (c *Client) CreateRelease(release *ct.Release) error {
	return c.Post("/releases", release, release)
}

// CreateApp creates a new app.
func (c *Client) CreateApp(app *ct.App) error {
	return c.Post("/apps", app, app)
}

// UpdateApp updates the protected flag and meta using app.ID.
func (c *Client) UpdateApp(app *ct.App) error {
	if app.ID == "" {
		return errors.New("controller: missing id")
	}
	return c.Post(fmt.Sprintf("/apps/%s", app.ID), app, app)
}

// DeleteApp deletes an app.
func (c *Client) DeleteApp(appID string) error {
	return c.Delete(fmt.Sprintf("/apps/%s", appID))
}

// CreateProvider creates a new provider.
func (c *Client) CreateProvider(provider *ct.Provider) error {
	return c.Post("/providers", provider, provider)
}

// GetProvider returns the provider identified by providerID.
func (c *Client) GetProvider(providerID string) (*ct.Provider, error) {
	provider := &ct.Provider{}
	return provider, c.Get(fmt.Sprintf("/providers/%s", providerID), provider)
}

// ProvisionResource uses a provider to provision a new resource for the
// application. Returns details about the resource.
func (c *Client) ProvisionResource(req *ct.ResourceReq) (*ct.Resource, error) {
	if req.ProviderID == "" {
		return nil, errors.New("controller: missing provider id")
	}
	res := &ct.Resource{}
	err := c.Post(fmt.Sprintf("/providers/%s/resources", req.ProviderID), req, res)
	return res, err
}

// GetResource returns the resource identified by resourceID under providerID.
func (c *Client) GetResource(providerID, resourceID string) (*ct.Resource, error) {
	res := &ct.Resource{}
	err := c.Get(fmt.Sprintf("/providers/%s/resources/%s", providerID, resourceID), res)
	return res, err
}

// ResourceList returns all resources under providerID.
func (c *Client) ResourceList(providerID string) ([]*ct.Resource, error) {
	var resources []*ct.Resource
	return resources, c.Get(fmt.Sprintf("/providers/%s/resources", providerID), &resources)
}

// AppResourceList returns a list of all resources under appID.
func (c *Client) AppResourceList(appID string) ([]*ct.Resource, error) {
	var resources []*ct.Resource
	return resources, c.Get(fmt.Sprintf("/apps/%s/resources", appID), &resources)
}

// PutResource updates a resource.
func (c *Client) PutResource(resource *ct.Resource) error {
	if resource.ID == "" || resource.ProviderID == "" {
		return errors.New("controller: missing id and/or provider id")
	}
	return c.Put(fmt.Sprintf("/providers/%s/resources/%s", resource.ProviderID, resource.ID), resource, resource)
}

// PutFormation updates an existing formation.
func (c *Client) PutFormation(formation *ct.Formation) error {
	if formation.AppID == "" || formation.ReleaseID == "" {
		return errors.New("controller: missing app id and/or release id")
	}
	return c.Put(fmt.Sprintf("/apps/%s/formations/%s", formation.AppID, formation.ReleaseID), formation, formation)
}

// PutJob updates an existing job.
func (c *Client) PutJob(job *ct.Job) error {
	if job.ID == "" || job.AppID == "" {
		return errors.New("controller: missing job id and/or app id")
	}
	return c.Put(fmt.Sprintf("/apps/%s/jobs/%s", job.AppID, job.ID), job, job)
}

// DeleteJob kills a specific job id under the specified app.
func (c *Client) DeleteJob(appID, jobID string) error {
	return c.Delete(fmt.Sprintf("/apps/%s/jobs/%s", appID, jobID))
}

// SetAppRelease sets the specified release as the current release for an app.
func (c *Client) SetAppRelease(appID, releaseID string) error {
	return c.Put(fmt.Sprintf("/apps/%s/release", appID), &ct.Release{ID: releaseID}, nil)
}

// GetAppRelease returns the current release of an app.
func (c *Client) GetAppRelease(appID string) (*ct.Release, error) {
	release := &ct.Release{}
	return release, c.Get(fmt.Sprintf("/apps/%s/release", appID), release)
}

// RouteList returns all routes for an app.
func (c *Client) RouteList(appID string) ([]*router.Route, error) {
	var routes []*router.Route
	return routes, c.Get(fmt.Sprintf("/apps/%s/routes", appID), &routes)
}

// GetRoute returns details for the routeID under the specified app.
func (c *Client) GetRoute(appID string, routeID string) (*router.Route, error) {
	route := &router.Route{}
	return route, c.Get(fmt.Sprintf("/apps/%s/routes/%s", appID, routeID), route)
}

// CreateRoute creates a new route for the specified app.
func (c *Client) CreateRoute(appID string, route *router.Route) error {
	return c.Post(fmt.Sprintf("/apps/%s/routes", appID), route, route)
}

// DeleteRoute deletes a route under the specified app.
func (c *Client) DeleteRoute(appID string, routeID string) error {
	return c.Delete(fmt.Sprintf("/apps/%s/routes/%s", appID, routeID))
}

// GetFormation returns details for the specified formation under app and
// release.
func (c *Client) GetFormation(appID, releaseID string) (*ct.Formation, error) {
	formation := &ct.Formation{}
	return formation, c.Get(fmt.Sprintf("/apps/%s/formations/%s", appID, releaseID), formation)
}

// FormationList returns a list of all formations under appID.
func (c *Client) FormationList(appID string) ([]*ct.Formation, error) {
	var formations []*ct.Formation
	return formations, c.Get(fmt.Sprintf("/apps/%s/formations", appID), &formations)
}

// DeleteFormation deletes the formation matching appID and releaseID.
func (c *Client) DeleteFormation(appID, releaseID string) error {
	return c.Delete(fmt.Sprintf("/apps/%s/formations/%s", appID, releaseID))
}

// GetRelease returns details for the specified release.
func (c *Client) GetRelease(releaseID string) (*ct.Release, error) {
	release := &ct.Release{}
	return release, c.Get(fmt.Sprintf("/releases/%s", releaseID), release)
}

// GetArtifact returns details for the specified artifact.
func (c *Client) GetArtifact(artifactID string) (*ct.Artifact, error) {
	artifact := &ct.Artifact{}
	return artifact, c.Get(fmt.Sprintf("/artifacts/%s", artifactID), artifact)
}

// GetApp returns details for the specified app.
func (c *Client) GetApp(appID string) (*ct.App, error) {
	app := &ct.App{}
	return app, c.Get(fmt.Sprintf("/apps/%s", appID), app)
}

// JobEventStream is a wrapper around an Events channel, allowing us to close
// the stream.
type JobEventStream struct {
	Events chan *ct.JobEvent
	body   io.ReadCloser
}

// Close closes the underlying stream.
func (s *JobEventStream) Close() {
	s.body.Close()
}

// StreamJobEvents returns a JobEventStream for an app.
func (c *Client) StreamJobEvents(appID string, lastID int64) (*JobEventStream, error) {
	header := http.Header{
		"Accept":        []string{"text/event-stream"},
		"Last-Event-Id": []string{strconv.FormatInt(lastID, 10)},
	}
	res, err := c.RawReq("GET", fmt.Sprintf("/apps/%s/jobs", appID), header, nil, nil)
	if err != nil {
		return nil, err
	}
	stream := &JobEventStream{Events: make(chan *ct.JobEvent), body: res.Body}
	go func() {
		defer close(stream.Events)
		dec := sse.NewDecoder(bufio.NewReader(stream.body))
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

// GetJobLog returns a ReadCloser stream of the job with id of jobID, running
// under appID. If tail is true, new log lines are streamed after the buffered
// log.
func (c *Client) GetJobLog(appID, jobID string, tail bool) (io.ReadCloser, error) {
	path := fmt.Sprintf("/apps/%s/jobs/%s/log", appID, jobID)
	if tail {
		path += "?tail=true"
	}
	res, err := c.RawReq("GET", path, nil, nil, nil)
	if err != nil {
		return nil, err
	}
	return res.Body, nil
}

// GetJobLogWithWait waits until the job is created, then returns a ReadCloser
// stream of the job with id of jobID, running under appID. If tail is true,
// new log lines are streamed after the buffered log.
func (c *Client) GetJobLogWithWait(appID, jobID string, tail bool) (io.ReadCloser, error) {
	path := fmt.Sprintf("/apps/%s/jobs/%s/log?wait=true", appID, jobID)
	if tail {
		path += "&tail=true"
	}
	res, err := c.RawReq("GET", path, nil, nil, nil)
	if err != nil {
		return nil, err
	}
	return res.Body, nil
}

// RunJobAttached runs a new job under the specified app, attaching to the job
// and returning a ReadWriteCloser stream, which can then be used for
// communicating with the job.
func (c *Client) RunJobAttached(appID string, job *ct.NewJob) (utils.ReadWriteCloser, error) {
	data, err := httpclient.ToJSON(job)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest("POST", fmt.Sprintf("%s/apps/%s/jobs", c.URL, appID), data)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/vnd.flynn.attach")
	req.SetBasicAuth("", c.Key)
	var dial rpcplus.DialFunc
	if c.Dial != nil {
		dial = c.Dial
	}
	res, rwc, err := utils.HijackRequest(req, dial)
	if err != nil {
		res.Body.Close()
		return nil, err
	}
	return rwc, nil
}

// RunJobDetached runs a new job under the specified app, returning the job's
// details.
func (c *Client) RunJobDetached(appID string, req *ct.NewJob) (*ct.Job, error) {
	job := &ct.Job{}
	return job, c.Post(fmt.Sprintf("/apps/%s/jobs", appID), req, job)
}

// GetJob returns a Job for the given app and job ID
func (c *Client) GetJob(appID, jobID string) (*ct.Job, error) {
	job := &ct.Job{}
	return job, c.Get(fmt.Sprintf("/apps/%s/jobs/%s", appID, jobID), job)
}

// JobList returns a list of all jobs.
func (c *Client) JobList(appID string) ([]*ct.Job, error) {
	var jobs []*ct.Job
	return jobs, c.Get(fmt.Sprintf("/apps/%s/jobs", appID), &jobs)
}

// AppList returns a list of all apps.
func (c *Client) AppList() ([]*ct.App, error) {
	var apps []*ct.App
	return apps, c.Get("/apps", &apps)
}

// KeyList returns a list of all ssh public keys added.
func (c *Client) KeyList() ([]*ct.Key, error) {
	var keys []*ct.Key
	return keys, c.Get("/keys", &keys)
}

// ArtifactList returns a list of all artifacts
func (c *Client) ArtifactList() ([]*ct.Artifact, error) {
	var artifacts []*ct.Artifact
	return artifacts, c.Get("/artifacts", &artifacts)
}

// ReleaseList returns a list of all releases
func (c *Client) ReleaseList() ([]*ct.Release, error) {
	var releases []*ct.Release
	return releases, c.Get("/releases", &releases)
}

// CreateKey uploads pubKey as the ssh public key.
func (c *Client) CreateKey(pubKey string) (*ct.Key, error) {
	key := &ct.Key{}
	return key, c.Post("/keys", &ct.Key{Key: pubKey}, key)
}

// GetKey returns details for the keyID.
func (c *Client) GetKey(keyID string) (*ct.Key, error) {
	key := &ct.Key{}
	return key, c.Get(fmt.Sprintf("/keys/%s", keyID), key)
}

// DeleteKey deletes a key with the specified id.
func (c *Client) DeleteKey(id string) error {
	return c.Delete("/keys/" + strings.Replace(id, ":", "", -1))
}

// ProviderList returns a list of all providers.
func (c *Client) ProviderList() ([]*ct.Provider, error) {
	var providers []*ct.Provider
	return providers, c.Get("/providers", &providers)
}
