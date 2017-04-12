package graphql

import (
	"time"

	ct "github.com/flynn/flynn/controller/types"
)

type appResolver struct {
	app *ct.App
}

func (r *appResolver) ID() string {
	return r.app.ID
}

func (r *appResolver) Name() string {
	return r.app.Name
}

func (r *appResolver) Meta() map[string]string {
	return r.app.Meta
}

// TODO(jvatic): Move this into controller/types
type DeploymentConfig struct {
	Strategy string
	Timeout  int32
}

func (r *appResolver) DeploymentConfig() *DeploymentConfig {
	return &DeploymentConfig{
		Strategy: r.app.Strategy,
		Timeout:  r.app.DeployTimeout,
	}
}

func (r *appResolver) CurrentRelease() *releaseResolver {
	return &releaseResolver{} // TODO(jvatic)
}

func (r *appResolver) UpdatedAt() *time.Time {
	return r.app.UpdatedAt
}

func (r *appResolver) CreatedAt() *time.Time {
	return r.app.CreatedAt
}

func (r *appResolver) Releases(args *struct {
	First  int
	After  string
	Last   int
	Before string
}) *releaseConnectionResolver {
	return &releaseConnectionResolver{} // TODO(jvatic)
}

func (r *appResolver) Formations(args *struct {
	First  int
	After  string
	Last   int
	Before string
}) *formationConnectionResolver {
	return &formationConnectionResolver{} // TODO(jvatic)
}

func (r *appResolver) Jobs(args *struct {
	First  int
	After  string
	Last   int
	Before string
}) *jobConnectionResolver {
	return &jobConnectionResolver{} // TODO(jvatic)
}

func (r *appResolver) Deployments(args *struct {
	First  int
	After  string
	Last   int
	Before string
}) *deploymentConnectionResolver {
	return &deploymentConnectionResolver{} // TODO(jvatic)
}

func (r *appResolver) Resources(args *struct {
	First  int
	After  string
	Last   int
	Before string
}) *resourceConnectionResolver {
	return &resourceConnectionResolver{} // TODO(jvatic)
}

func (r *appResolver) Routes(args *struct {
	First  int
	After  string
	Last   int
	Before string
}) *routeConnectionResolver {
	return &routeConnectionResolver{} // TODO(jvatic)
}

func (r *appResolver) Groups(args *struct {
	First  int
	After  string
	Last   int
	Before string
}) *groupConnectionResolver {
	return &groupConnectionResolver{} // TODO(jvatic)
}

type releaseResolver struct{}
type releaseConnectionResolver struct{}
type formationConnectionResolver struct{}
type jobConnectionResolver struct{}
type deploymentConnectionResolver struct{}
type resourceConnectionResolver struct{}
type routeConnectionResolver struct{}
type groupConnectionResolver struct{}
