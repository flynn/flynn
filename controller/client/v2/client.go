// Package v2controller provides a client for v2 of the controller API (GraphQL).
package v2controller

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/flynn/flynn/controller/client/v1"
	ct "github.com/flynn/flynn/controller/types"
	gt "github.com/flynn/flynn/controller/types/graphql"
	logagg "github.com/flynn/flynn/logaggregator/types"
	"github.com/flynn/flynn/pkg/httpclient"
	"github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/stream"
	"github.com/flynn/flynn/router/types"
	"github.com/graphql-go/handler"
)

// Client is a client for the v2 of the controller API (GraphQL).
type Client struct {
	*httpclient.Client

	v1client *v1controller.Client
}

func New(v1client *v1controller.Client) *Client {
	return &Client{
		Client:   v1client.Client,
		v1client: v1client,
	}
}

type graphqlResponse struct {
	Errors ct.GraphQLErrors `json:"errors"`
	Data   map[string]json.RawMessage
}

func (c *Client) graphqlRequest(body *handler.RequestOptions) (map[string]json.RawMessage, error) {
	out := &graphqlResponse{}
	if err := c.Post("/graphql", body, out); err != nil {
		return nil, err
	}
	if len(out.Errors) > 0 {
		// TODO(jvatic): Replace this block with better error handling on the server
		errString := out.Errors[0].Error()
		if errString == ct.ErrNotFound.Error() {
			return nil, ct.ErrNotFound
		}
		if strings.HasPrefix(errString, "validation error:") {
			parts := strings.SplitN(strings.TrimPrefix(errString, "validation error: "), " ", 2)
			var detail json.RawMessage
			if parts[0] != "" {
				detail, _ = json.Marshal(map[string]string{"field": parts[0]})
			}
			return nil, httphelper.JSONError{
				Code:    httphelper.ValidationErrorCode,
				Message: parts[1],
				Detail:  detail,
			}
		}
		return nil, out.Errors
	}
	return out.Data, nil
}

func (c *Client) GetCACert() ([]byte, error) {
	return c.v1client.GetCACert()
}

func (c *Client) StreamFormations(since *time.Time, output chan<- *ct.ExpandedFormation) (stream.Stream, error) {
	return c.v1client.StreamFormations(since, output)
}

func (c *Client) PutDomain(dm *ct.DomainMigration) error {
	return c.v1client.PutDomain(dm)
}

func (c *Client) CreateArtifact(artifact *ct.Artifact) error {
	data, err := c.graphqlRequest(&handler.RequestOptions{
		Query: `
			mutation createArtifactQuery($id: String, $type: String!, $uri: String!, $meta: MetaObject) {
				artifact: createArtifact(id: $id, type: $type, uri: $uri, meta: $meta) {
					id
					type
					uri
					meta
					created_at
				}
			}
		`,
		Variables: map[string]interface{}{
			"id":   artifact.ID,
			"type": artifact.Type,
			"uri":  artifact.URI,
			"meta": artifact.Meta,
		},
	})
	if err != nil {
		return err
	}
	return json.Unmarshal(data["artifact"], artifact)
}

func (c *Client) CreateRelease(release *ct.Release) error {
	data, err := c.graphqlRequest(&handler.RequestOptions{
		Query: `
			mutation createReleaseQuery($id: String, $artifacts: [String]!, $env: EnvObject, $meta: MetaObject, $processes: ProcessesObject) {
				release: createRelease(id: $id, artifacts: $artifacts, env: $env, meta: $meta, processes: $processes) {
					id
					artifacts {
						id
					}
					env
					meta
					processes
					created_at
				}
			}
		`,
		Variables: map[string]interface{}{
			"id":        release.ID,
			"artifacts": release.ArtifactIDs,
			"env":       release.Env,
			"meta":      release.Meta,
			"processes": release.Processes,
		},
	})
	if err != nil {
		return err
	}
	out := &gt.Release{}
	if err := json.Unmarshal(data["release"], out); err != nil {
		return err
	}
	r := out.ToStandardType()
	release.ID = r.ID
	release.ArtifactIDs = r.ArtifactIDs
	release.Env = r.Env
	release.Meta = r.Meta
	release.Processes = r.Processes
	release.CreatedAt = r.CreatedAt
	return nil
}

func (c *Client) CreateApp(app *ct.App) error {
	return c.v1client.CreateApp(app)
}

func (c *Client) UpdateApp(app *ct.App) error {
	return c.v1client.UpdateApp(app)
}

func (c *Client) UpdateAppMeta(app *ct.App) error {
	return c.v1client.UpdateAppMeta(app)
}

func (c *Client) DeleteApp(appID string) (*ct.AppDeletion, error) {
	return c.v1client.DeleteApp(appID)
}

func (c *Client) CreateProvider(provider *ct.Provider) error {
	return c.v1client.CreateProvider(provider)
}

func (c *Client) GetProvider(providerID string) (*ct.Provider, error) {
	data, err := c.graphqlRequest(&handler.RequestOptions{
		Query: fmt.Sprintf(`{
		provider(id: "%s") {
			id
			name
			url
			created_at
			updated_at
		}
	}`, providerID)})
	if err != nil {
		return nil, err
	}
	provider := &ct.Provider{}
	return provider, json.Unmarshal(data["provider"], provider)
}

func (c *Client) ProvisionResource(req *ct.ResourceReq) (*ct.Resource, error) {
	return c.v1client.ProvisionResource(req)
}

func (c *Client) GetResource(providerID, resourceID string) (*ct.Resource, error) {
	data, err := c.graphqlRequest(&handler.RequestOptions{
		Query: fmt.Sprintf(`{
			resource(id: "%s", provider: "%s") {
			id
			provider {
				id
			}
			external_id
			env
			apps {
				id
			}
			created_at
		}
	}`, resourceID, providerID)})
	if err != nil {
		return nil, err
	}
	resource := &gt.Resource{}
	if err := json.Unmarshal(data["resource"], resource); err != nil {
		return nil, err
	}
	return resource.ToStandardType(), nil
}

func (c *Client) ResourceListAll() ([]*ct.Resource, error) {
	data, err := c.graphqlRequest(&handler.RequestOptions{
		Query: `{
		resources {
			id
			provider {
				id
			}
			external_id
			env
			apps {
				id
			}
			created_at
		}
	}`})
	if err != nil {
		return nil, err
	}
	l := []*gt.Resource{}
	if err := json.Unmarshal(data["resources"], &l); err != nil {
		return nil, err
	}
	list := make([]*ct.Resource, len(l))
	for i, r := range l {
		list[i] = r.ToStandardType()
	}
	return list, nil
}

func (c *Client) ResourceList(providerID string) ([]*ct.Resource, error) {
	data, err := c.graphqlRequest(&handler.RequestOptions{
		Query: fmt.Sprintf(`{
		provider(id: "%s") {
			id
			resources {
				id
				external_id
				env
				apps {
					id
				}
				created_at
			}
		}
	}`, providerID)})
	if err != nil {
		return nil, err
	}
	provider := &gt.Provider{}
	if err := json.Unmarshal(data["provider"], provider); err != nil {
		return nil, err
	}
	list := make([]*ct.Resource, len(provider.Resources))
	for i, r := range provider.Resources {
		list[i] = r.ToStandardType()
		list[i].ProviderID = provider.ID
	}
	return list, nil
}

func (c *Client) AddResourceApp(providerID, resourceID, appID string) (*ct.Resource, error) {
	return c.v1client.AddResourceApp(providerID, resourceID, appID)
}

func (c *Client) DeleteResourceApp(providerID, resourceID, appID string) (*ct.Resource, error) {
	return c.v1client.DeleteResourceApp(providerID, resourceID, appID)
}

func (c *Client) AppResourceList(appID string) ([]*ct.Resource, error) {
	data, err := c.graphqlRequest(&handler.RequestOptions{
		Query: fmt.Sprintf(`{
		app(id: "%s") {
			resources {
				id
				provider {
					id
				}
				external_id
				env
				apps {
					id
				}
				created_at
			}
		}
	}`, appID)})
	if err != nil {
		return nil, err
	}
	app := &gt.App{}
	if err := json.Unmarshal(data["app"], app); err != nil {
		return nil, err
	}
	list := make([]*ct.Resource, len(app.Resources))
	for i, r := range app.Resources {
		list[i] = r.ToStandardType()
	}
	return list, nil
}

func (c *Client) PutResource(resource *ct.Resource) error {
	return c.v1client.PutResource(resource)
}

func (c *Client) DeleteResource(providerID, resourceID string) (*ct.Resource, error) {
	return c.v1client.DeleteResource(providerID, resourceID)
}

func (c *Client) PutFormation(formation *ct.Formation) error {
	data, err := c.graphqlRequest(&handler.RequestOptions{
		Query: `
			mutation putFormation($app: String!, $release: String!, $processes: ProcessesObject, $tags: TagsObject) {
				formation: putFormation(app: $app, release: $release, processes: $processes, tags: $tags) {
					app {
						id
					}
					release {
						id
					}
					processes
					tags
					updated_at
					created_at
				}
			}
		`,
		Variables: map[string]interface{}{
			"app":       formation.AppID,
			"release":   formation.ReleaseID,
			"processes": formation.Processes,
			"tags":      formation.Tags,
		},
	})
	if err != nil {
		return err
	}
	out := &gt.Formation{}
	if err := json.Unmarshal(data["formation"], out); err != nil {
		return err
	}
	f := out.ToStandardType()
	formation.AppID = f.AppID
	formation.ReleaseID = f.ReleaseID
	formation.Processes = f.Processes
	formation.Tags = f.Tags
	formation.UpdatedAt = f.UpdatedAt
	formation.CreatedAt = f.CreatedAt
	return nil
}

func (c *Client) PutJob(job *ct.Job) error {
	return c.v1client.PutJob(job)
}

func (c *Client) DeleteJob(appID, jobID string) error {
	return c.v1client.DeleteJob(appID, jobID)
}

func (c *Client) SetAppRelease(appID, releaseID string) error {
	return c.v1client.SetAppRelease(appID, releaseID)
}

func (c *Client) GetAppRelease(appID string) (*ct.Release, error) {
	data, err := c.graphqlRequest(&handler.RequestOptions{
		Query: fmt.Sprintf(`{
		app(id: "%s") {
			current_release {
				id
				artifacts {
					id
				}
				env
				meta
				processes
				created_at
			}
		}
	}`, appID)})
	if err != nil {
		return nil, err
	}
	app := &gt.App{}
	if err := json.Unmarshal(data["app"], &app); err != nil {
		return nil, err
	}
	return app.Release.ToStandardType(), nil
}

func (c *Client) RouteList(appID string) ([]*router.Route, error) {
	data, err := c.graphqlRequest(&handler.RequestOptions{
		Query: fmt.Sprintf(`{
		app(id: "%s") {
			routes {
				type
				id
				parent_ref
				service
				leader
				created_at
				updated_at
				domain
				certificate {
					id
					key
					cert
					created_at
					updated_at
				}
				sticky
				path
				port
			}
		}
	}`, appID)})
	if err != nil {
		return nil, err
	}
	app := &gt.App{}
	if err := json.Unmarshal(data["app"], app); err != nil {
		return nil, err
	}
	list := make([]*router.Route, len(app.Routes))
	for i, r := range app.Routes {
		list[i] = r.ToStandardType()
	}
	return list, nil
}

func (c *Client) GetRoute(appID string, routeID string) (*router.Route, error) {
	data, err := c.graphqlRequest(&handler.RequestOptions{
		Query: fmt.Sprintf(`{
		route(app: "%s", id: "%s") {
			type
			id
			parent_ref
			service
			leader
			created_at
			updated_at
			domain
			certificate {
				id
				key
				cert
				created_at
				updated_at
			}
			sticky
			path
			port
		}
	}`, appID, routeID)})
	if err != nil {
		return nil, err
	}
	route := &gt.Route{}
	if err := json.Unmarshal(data["route"], route); err != nil {
		return nil, err
	}
	return route.ToStandardType(), nil
}

func (c *Client) CreateRoute(appID string, route *router.Route) error {
	return c.v1client.CreateRoute(appID, route)
}

func (c *Client) UpdateRoute(appID string, routeID string, route *router.Route) error {
	return c.v1client.UpdateRoute(appID, routeID, route)
}

func (c *Client) DeleteRoute(appID string, routeID string) error {
	return c.v1client.DeleteRoute(appID, routeID)
}

func (c *Client) GetFormation(appID, releaseID string) (*ct.Formation, error) {
	data, err := c.graphqlRequest(&handler.RequestOptions{
		Query: fmt.Sprintf(`{
		formation(app: "%s", release: "%s") {
			app {
				id
			}
			release {
				id
			}
			processes
			tags
			updated_at
			created_at
		}
	}`, appID, releaseID)})
	if err != nil {
		return nil, err
	}
	formation := &gt.Formation{}
	if err := json.Unmarshal(data["formation"], formation); err != nil {
		return nil, err
	}
	return formation.ToStandardType(), nil
}

func (c *Client) GetExpandedFormation(appID, releaseID string) (*ct.ExpandedFormation, error) {
	data, err := c.graphqlRequest(&handler.RequestOptions{
		Query: fmt.Sprintf(`
		query {
			expanded_formation(app: "%s", release: "%s") {
				app {
					id
					name
					meta
				}
				image_artifact {
					...artifactFields
				}
				file_artifacts {
					...artifactFields
				}
				release {
					id
					artifacts {
						id
					}
					meta
					env
					processes
				}
				processes
				tags
				updated_at
			}
		}

		fragment artifactFields on Artifact {
			id
			type
			uri
			meta
			created_at
		}`, appID, releaseID)})
	if err != nil {
		return nil, err
	}
	formation := &gt.ExpandedFormation{}
	if err := json.Unmarshal(data["expanded_formation"], formation); err != nil {
		return nil, err
	}
	return formation.ToStandardType(), nil
}

func (c *Client) FormationList(appID string) ([]*ct.Formation, error) {
	data, err := c.graphqlRequest(&handler.RequestOptions{
		Query: fmt.Sprintf(`{
		app(id: "%s") {
			formations {
				app {
					id
				}
				release {
					id
				}
				processes
				tags
				updated_at
				created_at
			}
		}
	}`, appID)})
	if err != nil {
		return nil, err
	}
	app := &gt.App{}
	if err := json.Unmarshal(data["app"], app); err != nil {
		return nil, err
	}
	list := make([]*ct.Formation, len(app.Formations))
	for i, f := range app.Formations {
		list[i] = f.ToStandardType()
	}
	return list, nil
}

func (c *Client) FormationListActive() ([]*ct.ExpandedFormation, error) {
	data, err := c.graphqlRequest(&handler.RequestOptions{
		Query: `
		query {
			active_formations {
				app {
					id
					name
					meta
				}
				image_artifact {
					...artifactFields
				}
				file_artifacts {
					...artifactFields
				}
				release {
					id
					artifacts {
						id
					}
					meta
					env
					processes
				}
				processes
				tags
				updated_at
			}
		}

		fragment artifactFields on Artifact {
			id
			type
			uri
			meta
			created_at
		}
	`})
	if err != nil {
		return nil, err
	}
	l := []*gt.ExpandedFormation{}
	if err := json.Unmarshal(data["active_formations"], &l); err != nil {
		return nil, err
	}
	list := make([]*ct.ExpandedFormation, len(l))
	for i, f := range l {
		list[i] = f.ToStandardType()
	}
	return list, nil
}

func (c *Client) DeleteFormation(appID, releaseID string) error {
	return c.v1client.DeleteFormation(appID, releaseID)
}

func (c *Client) GetRelease(releaseID string) (*ct.Release, error) {
	data, err := c.graphqlRequest(&handler.RequestOptions{
		Query: fmt.Sprintf(`{
		release(id: "%s") {
			id
			artifacts {
				id
			}
			env
			meta
			processes
			created_at
		}
	}`, releaseID)})
	if err != nil {
		return nil, err
	}
	release := &gt.Release{}
	if err := json.Unmarshal(data["release"], &release); err != nil {
		return nil, err
	}
	return release.ToStandardType(), nil
}

func (c *Client) GetArtifact(artifactID string) (*ct.Artifact, error) {
	data, err := c.graphqlRequest(&handler.RequestOptions{
		Query: fmt.Sprintf(`{
		artifact(id: "%s") {
			id
			type
			uri
			meta
			created_at
		}
	}`, artifactID)})
	if err != nil {
		return nil, err
	}
	artifact := &ct.Artifact{}
	return artifact, json.Unmarshal(data["artifact"], artifact)
}

func (c *Client) GetApp(appID string) (*ct.App, error) {
	data, err := c.graphqlRequest(&handler.RequestOptions{
		Query: fmt.Sprintf(`{
		app(id: "%s") {
			id
			name
			meta
			strategy
			current_release {
				id
			}
			deploy_timeout
			created_at
			updated_at
		}
	}`, appID)})
	if err != nil {
		return nil, err
	}
	app := &gt.App{}
	if err := json.Unmarshal(data["app"], app); err != nil {
		return nil, err
	}
	return app.ToStandardType(), nil
}

func (c *Client) GetAppLog(appID string, options *logagg.LogOpts) (io.ReadCloser, error) {
	return c.v1client.GetAppLog(appID, options)
}

func (c *Client) StreamAppLog(appID string, options *logagg.LogOpts, output chan<- *ct.SSELogChunk) (stream.Stream, error) {
	return c.v1client.StreamAppLog(appID, options, output)
}

func (c *Client) GetDeployment(deploymentID string) (*ct.Deployment, error) {
	data, err := c.graphqlRequest(&handler.RequestOptions{
		Query: fmt.Sprintf(`{
		deployment(id: "%s") {
			id
			app {
				id
			}
			old_release {
				id
			}
			new_release {
				id
			}
			strategy
			status
			processes
			deploy_timeout
			created_at
			finished_at
		}
	}`, deploymentID)})
	if err != nil {
		return nil, err
	}
	deployment := &gt.Deployment{}
	if err := json.Unmarshal(data["deployment"], deployment); err != nil {
		return nil, err
	}
	return deployment.ToStandardType(), nil
}

func (c *Client) CreateDeployment(appID, releaseID string) (*ct.Deployment, error) {
	return c.v1client.CreateDeployment(appID, releaseID)
}

func (c *Client) DeploymentList(appID string) ([]*ct.Deployment, error) {
	data, err := c.graphqlRequest(&handler.RequestOptions{
		Query: fmt.Sprintf(`{
		app(id: "%s") {
			deployments {
				id
				app {
					id
				}
				old_release {
					id
				}
				new_release {
					id
				}
				strategy
				status
				processes
				deploy_timeout
				created_at
				finished_at
			}
		}
	}`, appID)})
	if err != nil {
		return nil, err
	}
	app := &gt.App{}
	if err := json.Unmarshal(data["app"], app); err != nil {
		return nil, err
	}
	list := make([]*ct.Deployment, len(app.Deployments))
	for i, d := range app.Deployments {
		list[i] = d.ToStandardType()
	}
	return list, nil
}

func (c *Client) StreamDeployment(d *ct.Deployment, output chan *ct.DeploymentEvent) (stream.Stream, error) {
	return c.v1client.StreamDeployment(d, output)
}

func (c *Client) DeployAppRelease(appID, releaseID string, stopWait <-chan struct{}) error {
	return c.v1client.DeployAppRelease(appID, releaseID, stopWait)
}

func (c *Client) StreamJobEvents(appID string, output chan *ct.Job) (stream.Stream, error) {
	return c.v1client.StreamJobEvents(appID, output)
}

func (c *Client) WatchJobEvents(appID, releaseID string) (ct.JobWatcher, error) {
	return c.v1client.WatchJobEvents(appID, releaseID)
}

func (c *Client) StreamEvents(opts ct.StreamEventsOptions, output chan *ct.Event) (stream.Stream, error) {
	return c.v1client.StreamEvents(opts, output)
}

func (c *Client) ListEvents(opts ct.ListEventsOptions) ([]*ct.Event, error) {
	return c.v1client.ListEvents(opts)
}

func (c *Client) GetEvent(id int64) (*ct.Event, error) {
	return c.v1client.GetEvent(id)
}

func (c *Client) ExpectedScalingEvents(actual, expected map[string]int, releaseProcesses map[string]ct.ProcessType, clusterSize int) ct.JobEvents {
	return c.v1client.ExpectedScalingEvents(actual, expected, releaseProcesses, clusterSize)
}

func (c *Client) RunJobAttached(appID string, job *ct.NewJob) (httpclient.ReadWriteCloser, error) {
	return c.v1client.RunJobAttached(appID, job)
}

func (c *Client) RunJobDetached(appID string, req *ct.NewJob) (*ct.Job, error) {
	return c.v1client.RunJobDetached(appID, req)
}

func (c *Client) GetJob(appID, jobID string) (*ct.Job, error) {
	data, err := c.graphqlRequest(&handler.RequestOptions{
		Query: fmt.Sprintf(`{
		job(app: "%s", id: "%s") {
			id
			uuid
			host_id
			app {
				id
			}
			release {
				id
			}
			type
			state
			args
			meta
			exit_status
			host_error
			run_at
			restarts
			created_at
			updated_at
		}
	}`, appID, jobID)})
	if err != nil {
		return nil, err
	}
	job := &gt.Job{}
	if err := json.Unmarshal(data["job"], job); err != nil {
		return nil, err
	}
	return job.ToStandardType(), nil
}

func (c *Client) JobList(appID string) ([]*ct.Job, error) {
	data, err := c.graphqlRequest(&handler.RequestOptions{
		Query: fmt.Sprintf(`{
		app(id: "%s") {
			id
			jobs {
				id
				uuid
				host_id
				release {
					id
				}
				type
				state
				args
				meta
				exit_status
				host_error
				run_at
				restarts
				created_at
				updated_at
			}
		}
	}`, appID)})
	if err != nil {
		return nil, err
	}
	app := &gt.App{}
	if err := json.Unmarshal(data["app"], app); err != nil {
		return nil, err
	}
	list := make([]*ct.Job, len(app.Jobs))
	for i, j := range app.Jobs {
		list[i] = j.ToStandardType()
		list[i].AppID = app.ID
	}
	return list, nil
}

func (c *Client) JobListActive() ([]*ct.Job, error) {
	data, err := c.graphqlRequest(&handler.RequestOptions{
		Query: `{
		active_jobs {
			id
			uuid
			host_id
			app {
				id
			}
			release {
				id
			}
			type
			state
			args
			meta
			exit_status
			host_error
			run_at
			restarts
			created_at
			updated_at
		}
	}`})
	if err != nil {
		return nil, err
	}
	l := []*gt.Job{}
	if err := json.Unmarshal(data["active_jobs"], &l); err != nil {
		return nil, err
	}
	list := make([]*ct.Job, len(l))
	for i, j := range l {
		list[i] = j.ToStandardType()
	}
	return list, nil
}

func (c *Client) AppList() ([]*ct.App, error) {
	data, err := c.graphqlRequest(&handler.RequestOptions{
		Query: `{
		apps {
			id
			name
			meta
			strategy
			current_release {
				id
			}
			deploy_timeout
			created_at
			updated_at
		}
	}`})
	if err != nil {
		return nil, err
	}
	l := []*gt.App{}
	if err := json.Unmarshal(data["apps"], &l); err != nil {
		return nil, err
	}
	list := make([]*ct.App, len(l))
	for i, a := range l {
		list[i] = a.ToStandardType()
	}
	return list, nil
}

func (c *Client) ArtifactList() ([]*ct.Artifact, error) {
	data, err := c.graphqlRequest(&handler.RequestOptions{
		Query: `{
		artifacts {
			id
			type
			uri
			meta
			created_at
		}
	}`})
	if err != nil {
		return nil, err
	}
	list := []*ct.Artifact{}
	return list, json.Unmarshal(data["artifacts"], &list)
}

func (c *Client) ReleaseList() ([]*ct.Release, error) {
	data, err := c.graphqlRequest(&handler.RequestOptions{
		Query: `{
		releases {
			id
			artifacts {
				id
			}
			env
			meta
			processes
			created_at
		}
	}`})
	if err != nil {
		return nil, err
	}
	l := []*gt.Release{}
	if err := json.Unmarshal(data["releases"], &l); err != nil {
		return nil, err
	}
	list := make([]*ct.Release, len(l))
	for i, r := range l {
		list[i] = r.ToStandardType()
	}
	return list, nil
}

func (c *Client) AppReleaseList(appID string) ([]*ct.Release, error) {
	data, err := c.graphqlRequest(&handler.RequestOptions{
		Query: fmt.Sprintf(`{
		app(id: "%s") {
			releases {
				id
				artifacts {
					id
				}
				env
				meta
				processes
				created_at
			}
		}
	}`, appID)})
	if err != nil {
		return nil, err
	}
	app := &gt.App{}
	if err := json.Unmarshal(data["app"], &app); err != nil {
		return nil, err
	}
	list := make([]*ct.Release, len(app.Releases))
	for i, r := range app.Releases {
		list[i] = r.ToStandardType()
	}
	return list, nil
}

func (c *Client) ProviderList() ([]*ct.Provider, error) {
	data, err := c.graphqlRequest(&handler.RequestOptions{
		Query: `{
		providers {
			id
			name
			url
			created_at
			updated_at
		}
	}`})
	if err != nil {
		return nil, err
	}
	list := []*ct.Provider{}
	return list, json.Unmarshal(data["providers"], &list)
}

func (c *Client) Backup() (io.ReadCloser, error) {
	return c.v1client.Backup()
}

func (c *Client) GetBackupMeta() (*ct.ClusterBackup, error) {
	return c.v1client.GetBackupMeta()
}

func (c *Client) DeleteRelease(appID, releaseID string) (*ct.ReleaseDeletion, error) {
	return c.v1client.DeleteRelease(appID, releaseID)
}

func (c *Client) ScheduleAppGarbageCollection(appID string) error {
	return c.v1client.ScheduleAppGarbageCollection(appID)
}
