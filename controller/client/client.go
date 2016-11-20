// Package controller provides a client for each version of the controller API.
package controller

import (
	"crypto/tls"
	"errors"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/flynn/flynn/controller/client/v1"
	ct "github.com/flynn/flynn/controller/types"
	logagg "github.com/flynn/flynn/logaggregator/types"
	"github.com/flynn/flynn/pkg/httpclient"
	"github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/pinned"
	"github.com/flynn/flynn/pkg/status"
	"github.com/flynn/flynn/pkg/stream"
	"github.com/flynn/flynn/router/types"
)

type Client interface {
	SetKey(newKey string)
	GetCACert() ([]byte, error)
	StreamFormations(since *time.Time, output chan<- *ct.ExpandedFormation) (stream.Stream, error)
	PutDomain(dm *ct.DomainMigration) error
	CreateArtifact(artifact *ct.Artifact) error
	CreateRelease(release *ct.Release) error
	CreateApp(app *ct.App) error
	UpdateApp(app *ct.App) error
	UpdateAppMeta(app *ct.App) error
	DeleteApp(appID string) (*ct.AppDeletion, error)
	CreateProvider(provider *ct.Provider) error
	GetProvider(providerID string) (*ct.Provider, error)
	ProvisionResource(req *ct.ResourceReq) (*ct.Resource, error)
	GetResource(providerID, resourceID string) (*ct.Resource, error)
	ResourceListAll() ([]*ct.Resource, error)
	ResourceList(providerID string) ([]*ct.Resource, error)
	AddResourceApp(providerID, resourceID, appID string) (*ct.Resource, error)
	DeleteResourceApp(providerID, resourceID, appID string) (*ct.Resource, error)
	AppResourceList(appID string) ([]*ct.Resource, error)
	PutResource(resource *ct.Resource) error
	DeleteResource(providerID, resourceID string) (*ct.Resource, error)
	PutFormation(formation *ct.Formation) error
	PutJob(job *ct.Job) error
	DeleteJob(appID, jobID string) error
	SetAppRelease(appID, releaseID string) error
	GetAppRelease(appID string) (*ct.Release, error)
	RouteList(appID string) ([]*router.Route, error)
	GetRoute(appID string, routeID string) (*router.Route, error)
	CreateRoute(appID string, route *router.Route) error
	UpdateRoute(appID string, routeID string, route *router.Route) error
	DeleteRoute(appID string, routeID string) error
	GetFormation(appID, releaseID string) (*ct.Formation, error)
	GetExpandedFormation(appID, releaseID string) (*ct.ExpandedFormation, error)
	FormationList(appID string) ([]*ct.Formation, error)
	FormationListActive() ([]*ct.ExpandedFormation, error)
	DeleteFormation(appID, releaseID string) error
	GetRelease(releaseID string) (*ct.Release, error)
	GetArtifact(artifactID string) (*ct.Artifact, error)
	GetApp(appID string) (*ct.App, error)
	GetAppLog(appID string, options *logagg.LogOpts) (io.ReadCloser, error)
	StreamAppLog(appID string, options *logagg.LogOpts, output chan<- *ct.SSELogChunk) (stream.Stream, error)
	GetDeployment(deploymentID string) (*ct.Deployment, error)
	CreateDeployment(appID, releaseID string) (*ct.Deployment, error)
	DeploymentList(appID string) ([]*ct.Deployment, error)
	StreamDeployment(d *ct.Deployment, output chan *ct.DeploymentEvent) (stream.Stream, error)
	DeployAppRelease(appID, releaseID string, stopWait <-chan struct{}) error
	StreamJobEvents(appID string, output chan *ct.Job) (stream.Stream, error)
	WatchJobEvents(appID, releaseID string) (ct.JobWatcher, error)
	StreamEvents(opts ct.StreamEventsOptions, output chan *ct.Event) (stream.Stream, error)
	ListEvents(opts ct.ListEventsOptions) ([]*ct.Event, error)
	GetEvent(id int64) (*ct.Event, error)
	ExpectedScalingEvents(actual, expected map[string]int, releaseProcesses map[string]ct.ProcessType, clusterSize int) ct.JobEvents
	RunJobAttached(appID string, job *ct.NewJob) (httpclient.ReadWriteCloser, error)
	RunJobDetached(appID string, req *ct.NewJob) (*ct.Job, error)
	GetJob(appID, jobID string) (*ct.Job, error)
	JobList(appID string) ([]*ct.Job, error)
	JobListActive() ([]*ct.Job, error)
	AppList() ([]*ct.App, error)
	ArtifactList() ([]*ct.Artifact, error)
	ReleaseList() ([]*ct.Release, error)
	AppReleaseList(appID string) ([]*ct.Release, error)
	ProviderList() ([]*ct.Provider, error)
	Backup() (io.ReadCloser, error)
	GetBackupMeta() (*ct.ClusterBackup, error)
	DeleteRelease(appID, releaseID string) (*ct.ReleaseDeletion, error)
	ScheduleAppGarbageCollection(appID string) error
	Status() (*status.Status, error)
}

type Config struct {
	Pin    []byte
	Domain string
}

// ErrNotFound is returned when a resource is not found (HTTP status 404).
var ErrNotFound = errors.New("controller: resource not found")

// newClient creates a generic Client object, additional attributes must
// be set by the caller
func newClient(key string, url string, http *http.Client) *v1controller.Client {
	c := &v1controller.Client{
		Client: &httpclient.Client{
			ErrNotFound: ErrNotFound,
			Key:         key,
			URL:         url,
			HTTP:        http,
		},
	}
	return c
}

// NewClient creates a new Client pointing at uri and using key for
// authentication.
func NewClient(uri, key string) (Client, error) {
	return NewClientWithHTTP(uri, key, httphelper.RetryClient)
}

func NewClientWithHTTP(uri, key string, httpClient *http.Client) (Client, error) {
	if uri == "" {
		uri = "http://controller.discoverd"
	}
	u, err := url.Parse(uri)
	if err != nil {
		return nil, err
	}
	return newClient(key, u.String(), httpClient), nil
}

// NewClientWithConfig acts like NewClient, but supports custom configuration.
func NewClientWithConfig(uri, key string, config Config) (Client, error) {
	if config.Pin == nil {
		return NewClient(uri, key)
	}
	d := &pinned.Config{Pin: config.Pin}
	if config.Domain != "" {
		d.Config = &tls.Config{ServerName: config.Domain}
	}
	httpClient := &http.Client{Transport: &http.Transport{DialTLS: d.Dial}}
	c := newClient(key, uri, httpClient)
	c.Host = config.Domain
	c.HijackDial = d.Dial
	return c, nil
}
