package main

import (
	"net/http"

	"github.com/flynn/flynn/controller/graphql"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/router/types"
	graphqlhandler "github.com/graphql-go/handler"
	"golang.org/x/net/context"
)

var graphqlSchema = graphqlschema.Schema

type graphqlDatabaseProxy struct {
	api *controllerAPI
}

func (p *graphqlDatabaseProxy) GetApp(id string) (*ct.App, error) {
	app, err := p.api.appRepo.Get(id)
	if err != nil {
		return nil, err
	}
	return app.(*ct.App), nil
}

func (p *graphqlDatabaseProxy) GetAppRelease(appID string) (*ct.Release, error) {
	return p.api.appRepo.GetRelease(appID)
}

func (p *graphqlDatabaseProxy) ListApps() ([]*ct.App, error) {
	list, err := p.api.appRepo.List()
	if err != nil {
		return nil, err
	}
	return list.([]*ct.App), nil
}

func (p *graphqlDatabaseProxy) ListAppsWithIDs(appIDs ...string) ([]*ct.App, error) {
	list, err := p.api.appRepo.ListIDs(appIDs...)
	if err != nil {
		return nil, err
	}
	return list.([]*ct.App), nil
}

func (p *graphqlDatabaseProxy) ListAppDeployments(appID string) ([]*ct.Deployment, error) {
	return p.api.deploymentRepo.List(appID)
}

func (p *graphqlDatabaseProxy) ListAppFormations(appID string) ([]*ct.Formation, error) {
	return p.api.formationRepo.List(appID)
}

func (p *graphqlDatabaseProxy) ListAppJobs(appID string) ([]*ct.Job, error) {
	return p.api.jobRepo.List(appID)
}

func (p *graphqlDatabaseProxy) ListAppReleases(appID string) ([]*ct.Release, error) {
	return p.api.releaseRepo.AppList(appID)
}

func (p *graphqlDatabaseProxy) ListAppResources(appID string) ([]*ct.Resource, error) {
	return p.api.resourceRepo.AppList(appID)
}

func (p *graphqlDatabaseProxy) ListAppRoutes(appID string) ([]*router.Route, error) {
	return p.api.routerc.ListRoutes(routeParentRef(appID))
}

func (p *graphqlDatabaseProxy) GetRoute(appID, routeType, routeID string) (*router.Route, error) {
	return p.api.getRoute(appID, routeType, routeID)
}

func (p *graphqlDatabaseProxy) ListCertRoutes(certID string) ([]*router.Route, error) {
	return p.api.routerc.ListCertRoutes(certID)
}

func (p *graphqlDatabaseProxy) GetEvent(eventID int64) (*ct.Event, error) {
	return p.api.eventRepo.GetEvent(eventID)
}

func (p *graphqlDatabaseProxy) ListEvents(appID string, objectTypes []string, objectID string, beforeID *int64, sinceID *int64, count int) ([]*ct.Event, error) {
	return p.api.eventRepo.ListEvents(appID, objectTypes, objectID, beforeID, sinceID, count)
}

func (p *graphqlDatabaseProxy) GetProvider(providerID string) (*ct.Provider, error) {
	provider, err := p.api.providerRepo.Get(providerID)
	if err != nil {
		return nil, err
	}
	return provider.(*ct.Provider), nil
}

func (p *graphqlDatabaseProxy) ListProviders() ([]*ct.Provider, error) {
	list, err := p.api.providerRepo.List()
	if err != nil {
		return nil, err
	}
	return list.([]*ct.Provider), nil
}

func (p *graphqlDatabaseProxy) GetResource(resourceID string) (*ct.Resource, error) {
	return p.api.resourceRepo.Get(resourceID)
}

func (p *graphqlDatabaseProxy) ListResources() ([]*ct.Resource, error) {
	return p.api.resourceRepo.List()
}

func (p *graphqlDatabaseProxy) ListProviderResources(providerID string) ([]*ct.Resource, error) {
	return p.api.resourceRepo.ProviderList(providerID)
}

func (p *graphqlDatabaseProxy) GetJob(jobID string) (*ct.Job, error) {
	return p.api.jobRepo.Get(jobID)
}

func (p *graphqlDatabaseProxy) ListActiveJobs() ([]*ct.Job, error) {
	return p.api.jobRepo.ListActive()
}

func (p *graphqlDatabaseProxy) GetDeployment(deploymentID string) (*ct.Deployment, error) {
	return p.api.deploymentRepo.Get(deploymentID)
}

func (p *graphqlDatabaseProxy) GetFormation(appID, releaseID string) (*ct.Formation, error) {
	return p.api.formationRepo.Get(appID, releaseID)
}

func (p *graphqlDatabaseProxy) GetExpandedFormation(appID, releaseID string, includeDeleted bool) (*ct.ExpandedFormation, error) {
	return p.api.formationRepo.GetExpanded(appID, releaseID, includeDeleted)
}

func (p *graphqlDatabaseProxy) ListActiveFormations() ([]*ct.ExpandedFormation, error) {
	return p.api.formationRepo.ListActive()
}

func (p *graphqlDatabaseProxy) GetRelease(releaseID string) (*ct.Release, error) {
	release, err := p.api.releaseRepo.Get(releaseID)
	if err != nil {
		return nil, err
	}
	return release.(*ct.Release), nil
}

func (p *graphqlDatabaseProxy) GetDeletedRelease(releaseID string) (*ct.Release, error) {
	release, err := p.api.releaseRepo.GetDeleted(releaseID)
	if err != nil {
		return nil, err
	}
	return release.(*ct.Release), nil
}

func (p *graphqlDatabaseProxy) ListReleases() ([]*ct.Release, error) {
	list, err := p.api.releaseRepo.List()
	if err != nil {
		return nil, err
	}
	return list.([]*ct.Release), nil
}

func (p *graphqlDatabaseProxy) GetArtifact(artifactID string) (*ct.Artifact, error) {
	artifact, err := p.api.artifactRepo.Get(artifactID)
	if err != nil {
		return nil, err
	}
	return artifact.(*ct.Artifact), nil
}

func (p *graphqlDatabaseProxy) ListArtifacts() ([]*ct.Artifact, error) {
	list, err := p.api.artifactRepo.List()
	if err != nil {
		return nil, err
	}
	return list.([]*ct.Artifact), nil
}

func (p *graphqlDatabaseProxy) ListArtifactsWithIDs(artifactIDs ...string) (map[string]*ct.Artifact, error) {
	return p.api.artifactRepo.ListIDs(artifactIDs...)
}

func (p *graphqlDatabaseProxy) AddArtifact(artifact *ct.Artifact) error {
	return p.api.artifactRepo.Add(artifact)
}

func (p *graphqlDatabaseProxy) AddRelease(release *ct.Release) error {
	return p.api.releaseRepo.Add(release)
}

func (p *graphqlDatabaseProxy) AddFormation(formation *ct.Formation) error {
	return p.api.formationRepo.Add(formation)
}

// Ensure graphqlDatabaseProxy implements DatabaseProxy
var _ graphqlschema.DatabaseProxy = (*graphqlDatabaseProxy)(nil)

func contextWithDatabaseProxy(api *controllerAPI, ctx context.Context) context.Context {
	proxy := &graphqlDatabaseProxy{api: api}
	ctx = context.WithValue(ctx, graphqlschema.DatabaseProxyContextKey, proxy)
	return ctx
}

func (api *controllerAPI) GraphQLHandler() httphelper.HandlerFunc {
	h := graphqlhandler.New(&graphqlhandler.Config{
		Schema: &graphqlSchema,
		Pretty: false,
	})
	return func(ctx context.Context, w http.ResponseWriter, req *http.Request) {
		h.ContextHandler(contextWithDatabaseProxy(api, ctx), w, req)
	}
}
