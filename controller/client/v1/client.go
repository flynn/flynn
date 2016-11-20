// Package v1controller provides a client for v1 of the controller API.
package v1controller

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"

	ct "github.com/flynn/flynn/controller/types"
	logagg "github.com/flynn/flynn/logaggregator/types"
	"github.com/flynn/flynn/pkg/httpclient"
	"github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/status"
	"github.com/flynn/flynn/pkg/stream"
	"github.com/flynn/flynn/router/types"
)

// Client is a client for the v1 of the controller API.
type Client struct {
	*httpclient.Client
}

func (c *Client) SetKey(newKey string) {
	c.Key = newKey
}

type jobWatcher struct {
	events    chan *ct.Job
	stream    stream.Stream
	releaseID string
}

func (w *jobWatcher) WaitFor(expected ct.JobEvents, timeout time.Duration, callback func(*ct.Job) error) error {
	actual := make(ct.JobEvents)
	timeoutCh := time.After(timeout)
	for {
		select {
		case e, ok := <-w.events:
			if !ok {
				if err := w.stream.Err(); err != nil {
					return err
				}
				return fmt.Errorf("Event stream unexpectedly ended")
			}
			if _, ok := actual[e.Type]; !ok {
				actual[e.Type] = make(map[ct.JobState]int)
			}
			if w.releaseID != "" && w.releaseID != e.ReleaseID {
				continue
			}
			// treat the legacy "crashed" and "failed" states as "down"
			if e.State == ct.JobStateCrashed || e.State == ct.JobStateFailed {
				e.State = ct.JobStateDown
			}
			actual[e.Type][e.State] += 1
			if callback != nil {
				err := callback(e)
				if err != nil {
					return err
				}
			}
			if jobEventsEqual(expected, actual) {
				return nil
			}
		case <-timeoutCh:
			return fmt.Errorf("Timed out waiting for job events. Waited %.f seconds.\nexpected: %v\nactual: %v", timeout.Seconds(), expected, actual)
		}
	}
}

func (w *jobWatcher) Close() error {
	return w.stream.Close()
}

func newJobWatcher(events chan *ct.Job, stream stream.Stream, releaseID string) ct.JobWatcher {
	w := &jobWatcher{
		events:    events,
		stream:    stream,
		releaseID: releaseID,
	}
	return w
}

func jobEventsEqual(expected, actual ct.JobEvents) bool {
	for typ, events := range expected {
		diff, ok := actual[typ]
		if !ok {
			if len(events) == 0 {
				continue
			}
			return false
		}
		for state, count := range events {
			actualCount, ok := diff[state]
			if !ok || actualCount != count {
				return false
			}
		}
	}
	return true
}

// GetCACert returns the CA cert for the controller
func (c *Client) GetCACert() ([]byte, error) {
	var cert bytes.Buffer
	res, err := c.RawReq("GET", "/ca-cert", nil, nil, nil)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if _, err := io.Copy(&cert, res.Body); err != nil {
		return nil, err
	}
	return cert.Bytes(), nil
}

// StreamFormations yields a series of ExpandedFormation into the provided channel.
// If since is not nil, only retrieves formation updates since the specified time.
func (c *Client) StreamFormations(since *time.Time, output chan<- *ct.ExpandedFormation) (stream.Stream, error) {
	if since == nil {
		s := time.Unix(0, 0)
		since = &s
	}
	t := since.UTC().Format(time.RFC3339Nano)
	return c.Stream("GET", "/formations?since="+t, nil, output)
}

// PutDomain migrates the cluster domain
func (c *Client) PutDomain(dm *ct.DomainMigration) error {
	if dm.Domain == "" {
		return errors.New("controller: missing domain")
	}
	if dm.OldDomain == "" {
		return errors.New("controller: missing old domain")
	}
	return c.Put("/domain", dm, dm)
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

// UpdateApp updates the meta and strategy using app.ID.
func (c *Client) UpdateApp(app *ct.App) error {
	if app.ID == "" {
		return errors.New("controller: missing id")
	}
	return c.Post(fmt.Sprintf("/apps/%s", app.ID), app, app)
}

// UpdateAppMeta updates the meta using app.ID, allowing empty meta to be set explicitly.
func (c *Client) UpdateAppMeta(app *ct.App) error {
	if app.ID == "" {
		return errors.New("controller: missing id")
	}
	return c.Post(fmt.Sprintf("/apps/%s/meta", app.ID), app, app)
}

// DeleteApp deletes an app.
func (c *Client) DeleteApp(appID string) (*ct.AppDeletion, error) {
	events := make(chan *ct.Event)
	stream, err := c.StreamEvents(ct.StreamEventsOptions{
		AppID:       appID,
		ObjectTypes: []ct.EventType{ct.EventTypeAppDeletion},
	}, events)
	if err != nil {
		return nil, err
	}
	defer stream.Close()

	if err := c.Delete(fmt.Sprintf("/apps/%s", appID), nil); err != nil {
		return nil, err
	}

	select {
	case event, ok := <-events:
		if !ok {
			return nil, stream.Err()
		}
		var e ct.AppDeletionEvent
		if err := json.Unmarshal(event.Data, &e); err != nil {
			return nil, err
		}
		if e.Error != "" {
			return nil, errors.New(e.Error)
		}
		return e.AppDeletion, nil
	case <-time.After(60 * time.Second):
		return nil, errors.New("timed out waiting for app deletion")
	}
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

// ResourceListAll returns all resources.
func (c *Client) ResourceListAll() ([]*ct.Resource, error) {
	var resources []*ct.Resource
	return resources, c.Get("/resources", &resources)
}

// ResourceList returns all resources under providerID.
func (c *Client) ResourceList(providerID string) ([]*ct.Resource, error) {
	var resources []*ct.Resource
	return resources, c.Get(fmt.Sprintf("/providers/%s/resources", providerID), &resources)
}

// AddResourceApp adds appID to the resource identified by resourceID and returns the resource
func (c *Client) AddResourceApp(providerID, resourceID, appID string) (*ct.Resource, error) {
	var resource *ct.Resource
	return resource, c.Put(fmt.Sprintf("/providers/%s/resources/%s/apps/%s", providerID, resourceID, appID), nil, &resource)
}

// DeleteResourceApp removes appID from the resource identified by resourceID and returns the resource
func (c *Client) DeleteResourceApp(providerID, resourceID, appID string) (*ct.Resource, error) {
	var resource *ct.Resource
	return resource, c.Delete(fmt.Sprintf("/providers/%s/resources/%s/apps/%s", providerID, resourceID, appID), &resource)
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

// DeleteResource deprovisions and deletes the resource identified by resourceID under providerID.
func (c *Client) DeleteResource(providerID, resourceID string) (*ct.Resource, error) {
	res := &ct.Resource{}
	err := c.Delete(fmt.Sprintf("/providers/%s/resources/%s", providerID, resourceID), res)
	return res, err
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
	if job.UUID == "" || job.AppID == "" {
		return errors.New("controller: missing job uuid and/or app id")
	}
	return c.Put(fmt.Sprintf("/apps/%s/jobs/%s", job.AppID, job.UUID), job, job)
}

// DeleteJob kills a specific job id under the specified app.
func (c *Client) DeleteJob(appID, jobID string) error {
	return c.Delete(fmt.Sprintf("/apps/%s/jobs/%s", appID, jobID), nil)
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

// UpdateRoute updates details for the routeID under the specified app.
func (c *Client) UpdateRoute(appID string, routeID string, route *router.Route) error {
	return c.Put(fmt.Sprintf("/apps/%s/routes/%s", appID, routeID), route, route)
}

// DeleteRoute deletes a route under the specified app.
func (c *Client) DeleteRoute(appID string, routeID string) error {
	return c.Delete(fmt.Sprintf("/apps/%s/routes/%s", appID, routeID), nil)
}

// GetFormation returns details for the specified formation under app and
// release.
func (c *Client) GetFormation(appID, releaseID string) (*ct.Formation, error) {
	formation := &ct.Formation{}
	return formation, c.Get(fmt.Sprintf("/apps/%s/formations/%s", appID, releaseID), formation)
}

// GetExpandedFormation returns expanded details for the specified formation
// under app and release.
func (c *Client) GetExpandedFormation(appID, releaseID string) (*ct.ExpandedFormation, error) {
	formation := &ct.ExpandedFormation{}
	return formation, c.Get(fmt.Sprintf("/apps/%s/formations/%s?expand=true", appID, releaseID), formation)
}

// FormationList returns a list of all formations under appID.
func (c *Client) FormationList(appID string) ([]*ct.Formation, error) {
	var formations []*ct.Formation
	return formations, c.Get(fmt.Sprintf("/apps/%s/formations", appID), &formations)
}

// FormationListActive returns a list of all active formations (i.e. formations
// whose process count is greater than zero).
func (c *Client) FormationListActive() ([]*ct.ExpandedFormation, error) {
	var formations []*ct.ExpandedFormation
	return formations, c.Get("/formations?active=true", &formations)
}

// DeleteFormation deletes the formation matching appID and releaseID.
func (c *Client) DeleteFormation(appID, releaseID string) error {
	return c.Delete(fmt.Sprintf("/apps/%s/formations/%s", appID, releaseID), nil)
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

// GetAppLog returns a ReadCloser log stream of the app with ID appID. If lines
// is zero or above, the number of lines returned will be capped at that value.
// Otherwise, all available logs are returned. If follow is true, new log lines
// are streamed after the buffered log.
func (c *Client) GetAppLog(appID string, opts *logagg.LogOpts) (io.ReadCloser, error) {
	path := fmt.Sprintf("/apps/%s/log", appID)
	if opts != nil {
		if encodedQuery := opts.EncodedQuery(); encodedQuery != "" {
			path = fmt.Sprintf("%s?%s", path, encodedQuery)
		}
	}
	res, err := c.RawReq("GET", path, nil, nil, nil)
	if err != nil {
		return nil, err
	}
	return res.Body, nil
}

// StreamAppLog is the same as GetAppLog but returns log lines via an SSE stream
func (c *Client) StreamAppLog(appID string, opts *logagg.LogOpts, output chan<- *ct.SSELogChunk) (stream.Stream, error) {
	path := fmt.Sprintf("/apps/%s/log", appID)
	if opts != nil {
		if encodedQuery := opts.EncodedQuery(); encodedQuery != "" {
			path = fmt.Sprintf("%s?%s", path, encodedQuery)
		}
	}
	return c.Stream("GET", path, nil, output)
}

// GetDeployment returns a deployment queued on the deployer.
func (c *Client) GetDeployment(deploymentID string) (*ct.Deployment, error) {
	res := &ct.Deployment{}
	return res, c.Get(fmt.Sprintf("/deployments/%s", deploymentID), res)
}

func (c *Client) CreateDeployment(appID, releaseID string) (*ct.Deployment, error) {
	deployment := &ct.Deployment{}
	return deployment, c.Post(fmt.Sprintf("/apps/%s/deploy", appID), &ct.Release{ID: releaseID}, deployment)
}

// DeploymentList returns a list of all deployments.
func (c *Client) DeploymentList(appID string) ([]*ct.Deployment, error) {
	var deployments []*ct.Deployment
	return deployments, c.Get(fmt.Sprintf("/apps/%s/deployments", appID), &deployments)
}

func convertEvents(appEvents chan *ct.Event, outputCh interface{}) {
	outValue := reflect.ValueOf(outputCh)
	msgType := outValue.Type().Elem().Elem()
	defer outValue.Close()
	for {
		a, ok := <-appEvents
		if !ok {
			return
		}
		e := reflect.New(msgType)
		if err := json.Unmarshal(a.Data, e.Interface()); err != nil {
			return
		}
		outValue.Send(e)
	}
}

func (c *Client) StreamDeployment(d *ct.Deployment, output chan *ct.DeploymentEvent) (stream.Stream, error) {
	appEvents := make(chan *ct.Event)
	go convertEvents(appEvents, output)
	return c.StreamEvents(ct.StreamEventsOptions{
		AppID:       d.AppID,
		ObjectID:    d.ID,
		ObjectTypes: []ct.EventType{ct.EventTypeDeployment},
		Past:        true,
	}, appEvents)
}

func (c *Client) DeployAppRelease(appID, releaseID string, stopWait <-chan struct{}) error {
	d, err := c.CreateDeployment(appID, releaseID)
	if err != nil {
		return err
	}

	// if initial deploy, just stop here
	if d.FinishedAt != nil {
		return nil
	}

	events := make(chan *ct.DeploymentEvent)
	stream, err := c.StreamDeployment(d, events)
	if err != nil {
		return err
	}
	defer stream.Close()

outer:
	for {
		select {
		case e, ok := <-events:
			if !ok {
				return fmt.Errorf("unexpected close of deployment event stream: %s", stream.Err())
			}
			switch e.Status {
			case "complete":
				break outer
			case "failed":
				return e.Err()
			}
		case <-stopWait:
			return errors.New("deploy wait cancelled")

		}
	}
	return nil
}

// StreamJobEvents streams job events to the output channel.
func (c *Client) StreamJobEvents(appID string, output chan *ct.Job) (stream.Stream, error) {
	appEvents := make(chan *ct.Event)
	go convertEvents(appEvents, output)
	return c.StreamEvents(ct.StreamEventsOptions{
		AppID:       appID,
		ObjectTypes: []ct.EventType{ct.EventTypeJob},
	}, appEvents)
}

func (c *Client) WatchJobEvents(appID, releaseID string) (ct.JobWatcher, error) {
	events := make(chan *ct.Job)
	stream, err := c.StreamJobEvents(appID, events)
	if err != nil {
		return nil, err
	}
	return newJobWatcher(events, stream, releaseID), nil
}

func (c *Client) StreamEvents(opts ct.StreamEventsOptions, output chan *ct.Event) (stream.Stream, error) {
	path, _ := url.Parse("/events")
	q := path.Query()
	if opts.AppID != "" {
		q.Set("app_id", opts.AppID)
	}
	if opts.Past {
		q.Set("past", "true")
	}
	if len(opts.ObjectTypes) > 0 {
		types := make([]string, len(opts.ObjectTypes))
		for i, t := range opts.ObjectTypes {
			types[i] = string(t)
		}
		q.Set("object_types", strings.Join(types, ","))
	}
	if opts.ObjectID != "" {
		q.Set("object_id", opts.ObjectID)
	}
	if opts.Count > 0 {
		q.Set("count", strconv.Itoa(opts.Count))
	}
	path.RawQuery = q.Encode()
	return c.ResumingStream("GET", path.String(), output)
}

func (c *Client) ListEvents(opts ct.ListEventsOptions) ([]*ct.Event, error) {
	var events []*ct.Event
	path, err := url.Parse("/events")
	if err != nil {
		return nil, err
	}
	q := path.Query()
	if opts.AppID != "" {
		q.Set("app_id", opts.AppID)
	}
	if opts.BeforeID != nil {
		q.Set("before_id", strconv.FormatInt(*opts.BeforeID, 10))
	}
	if opts.SinceID != nil {
		q.Set("since_id", strconv.FormatInt(*opts.SinceID, 10))
	}
	if len(opts.ObjectTypes) > 0 {
		types := make([]string, len(opts.ObjectTypes))
		for i, t := range opts.ObjectTypes {
			types[i] = string(t)
		}
		q.Set("object_types", strings.Join(types, ","))
	}
	if opts.ObjectID != "" {
		q.Set("object_id", opts.ObjectID)
	}
	if opts.Count > 0 {
		q.Set("count", strconv.Itoa(opts.Count))
	}
	path.RawQuery = q.Encode()
	h := make(http.Header)
	h.Set("Accept", "application/json")
	res, err := c.RawReq("GET", path.String(), h, nil, &events)
	if err != nil {
		return nil, err
	}
	res.Body.Close()
	return events, nil
}

func (c *Client) GetEvent(id int64) (*ct.Event, error) {
	var event *ct.Event
	return event, c.Get(fmt.Sprintf("/events/%d", id), &event)
}

func (c *Client) ExpectedScalingEvents(actual, expected map[string]int, releaseProcesses map[string]ct.ProcessType, clusterSize int) ct.JobEvents {
	events := make(ct.JobEvents, len(expected))
	for typ, count := range expected {
		diff := count
		val, ok := actual[typ]
		if ok {
			diff = count - val
		}
		proc, ok := releaseProcesses[typ]
		if ok && proc.Omni {
			diff *= clusterSize
		}
		if diff > 0 {
			events[typ] = ct.JobUpEvents(diff)
		} else if diff < 0 {
			events[typ] = ct.JobDownEvents(-diff)
		}
	}
	return events
}

// RunJobAttached runs a new job under the specified app, attaching to the job
// and returning a ReadWriteCloser stream, which can then be used for
// communicating with the job.
func (c *Client) RunJobAttached(appID string, job *ct.NewJob) (httpclient.ReadWriteCloser, error) {
	return c.Hijack("POST", fmt.Sprintf("/apps/%s/jobs", appID), http.Header{"Upgrade": {"flynn-attach/0"}}, job)
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

// JobListActive returns a list of all active jobs.
func (c *Client) JobListActive() ([]*ct.Job, error) {
	var jobs []*ct.Job
	return jobs, c.Get("/active-jobs", &jobs)
}

// AppList returns a list of all apps.
func (c *Client) AppList() ([]*ct.App, error) {
	var apps []*ct.App
	return apps, c.Get("/apps", &apps)
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

// AppReleaseList returns a list of all releases under appID.
func (c *Client) AppReleaseList(appID string) ([]*ct.Release, error) {
	var releases []*ct.Release
	return releases, c.Get(fmt.Sprintf("/apps/%s/releases", appID), &releases)
}

// ProviderList returns a list of all providers.
func (c *Client) ProviderList() ([]*ct.Provider, error) {
	var providers []*ct.Provider
	return providers, c.Get("/providers", &providers)
}

// Backup takes a backup of the cluster
func (c *Client) Backup() (io.ReadCloser, error) {
	res, err := c.RawReq("GET", "/backup", nil, nil, nil)
	if err != nil {
		return nil, err
	}
	return res.Body, nil
}

// GetBackupMeta returns metadata for latest backup
func (c *Client) GetBackupMeta() (*ct.ClusterBackup, error) {
	b := &ct.ClusterBackup{}
	return b, c.Get("/backup", b)
}

// DeleteRelease deletes a release and any associated file artifacts.
func (c *Client) DeleteRelease(appID, releaseID string) (*ct.ReleaseDeletion, error) {
	events := make(chan *ct.Event)
	stream, err := c.StreamEvents(ct.StreamEventsOptions{
		AppID:       appID,
		ObjectID:    releaseID,
		ObjectTypes: []ct.EventType{ct.EventTypeReleaseDeletion},
	}, events)
	if err != nil {
		return nil, err
	}
	defer stream.Close()

	if err := c.Delete(fmt.Sprintf("/apps/%s/releases/%s", appID, releaseID), nil); err != nil {
		return nil, err
	}

	select {
	case event, ok := <-events:
		if !ok {
			return nil, stream.Err()
		}
		var e ct.ReleaseDeletionEvent
		if err := json.Unmarshal(event.Data, &e); err != nil {
			return nil, err
		}
		if e.Error != "" {
			return nil, errors.New(e.Error)
		}
		return e.ReleaseDeletion, nil
	case <-time.After(60 * time.Second):
		return nil, errors.New("timed out waiting for release deletion")
	}
}

// ScheduleAppGarbageCollection schedules a garbage collection cycle for the app
func (c *Client) ScheduleAppGarbageCollection(appID string) error {
	return c.Post(fmt.Sprintf("/apps/%s/gc", appID), nil, nil)
}

// Status gets the controller status
func (c *Client) Status() (*status.Status, error) {
	type statusResponse struct {
		Data status.Status `json:"data"`
	}
	s := &statusResponse{}
	if err := c.Get(status.Path, s); err != nil {
		return nil, err
	}
	return &s.Data, nil
}

func (c *Client) Put(path string, in, out interface{}) error {
	return c.send("PUT", path, in, out)
}

func (c *Client) Post(path string, in, out interface{}) error {
	return c.send("POST", path, in, out)
}

func (c *Client) Get(path string, out interface{}) error {
	return c.send("GET", path, nil, out)
}

func (c *Client) Delete(path string, out interface{}) error {
	return c.send("DELETE", path, nil, out)
}

func (c *Client) send(method, path string, in, out interface{}) (err error) {
	for startTime := time.Now(); time.Since(startTime) < 10*time.Second; time.Sleep(100 * time.Millisecond) {
		err = c.Send(method, path, in, out)
		if !httphelper.IsRetryableError(err) {
			break
		}
	}
	return
}
