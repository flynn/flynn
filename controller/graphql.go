package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/shutdown"
	"github.com/flynn/flynn/router/types"
	"github.com/graphql-go/graphql"
	"github.com/graphql-go/graphql/language/ast"
	graphqlhandler "github.com/graphql-go/handler"
	"golang.org/x/net/context"
)

var graphqlSchema graphql.Schema

func newObjectType(name string) *graphql.Scalar {
	return graphql.NewScalar(graphql.ScalarConfig{
		Name: name,
		Serialize: func(value interface{}) interface{} {
			return value
		},
		ParseValue: func(value interface{}) interface{} {
			return value
		},
		ParseLiteral: func(valueAST ast.Value) interface{} {
			switch valueAST := valueAST.(type) {
			case *ast.ObjectValue:
				return valueAST.GetValue()
			}
			return nil
		},
	})
}

var (
	metaObjectType      = newObjectType("MetaObject")
	envObjectType       = newObjectType("EnvObject")
	processesObjectType = newObjectType("ProcessesObject")
	tagsObjectType      = newObjectType("TagsObject")
	eventDataObjectType = newObjectType("EventDataObject")
)

var graphqlTimeType = graphql.NewScalar(graphql.ScalarConfig{
	Name: "Time",
	Serialize: func(value interface{}) interface{} {
		if ts, ok := value.(*time.Time); ok {
			if data, err := ts.MarshalText(); err == nil {
				return string(data)
			}
		}
		if ts, ok := value.(time.Time); ok {
			if data, err := ts.MarshalText(); err == nil {
				return string(data)
			}
		}
		return nil
	},
	ParseValue: func(value interface{}) interface{} {
		if str, ok := value.(string); ok {
			var ts time.Time
			if err := ts.UnmarshalText([]byte(str)); err == nil {
				return &ts
			}
		}
		return nil
	},
	ParseLiteral: func(valueAST ast.Value) interface{} {
		switch valueAST := valueAST.(type) {
		case *ast.StringValue:
			return valueAST.GetValue()
		}
		return nil
	},
})

var eventObjectTypeEnum = graphql.NewEnum(graphql.EnumConfig{
	Name:        "EventType",
	Description: "Type of event",
	Values: graphql.EnumValueConfigMap{
		string(ct.EventTypeApp):                  &graphql.EnumValueConfig{Value: ct.EventTypeApp},
		string(ct.EventTypeAppDeletion):          &graphql.EnumValueConfig{Value: ct.EventTypeAppDeletion},
		string(ct.EventTypeAppRelease):           &graphql.EnumValueConfig{Value: ct.EventTypeAppRelease},
		string(ct.EventTypeDeployment):           &graphql.EnumValueConfig{Value: ct.EventTypeDeployment},
		string(ct.EventTypeJob):                  &graphql.EnumValueConfig{Value: ct.EventTypeJob},
		string(ct.EventTypeScale):                &graphql.EnumValueConfig{Value: ct.EventTypeScale},
		string(ct.EventTypeRelease):              &graphql.EnumValueConfig{Value: ct.EventTypeRelease},
		string(ct.EventTypeReleaseDeletion):      &graphql.EnumValueConfig{Value: ct.EventTypeReleaseDeletion},
		string(ct.EventTypeArtifact):             &graphql.EnumValueConfig{Value: ct.EventTypeArtifact},
		string(ct.EventTypeProvider):             &graphql.EnumValueConfig{Value: ct.EventTypeProvider},
		string(ct.EventTypeResource):             &graphql.EnumValueConfig{Value: ct.EventTypeResource},
		string(ct.EventTypeResourceDeletion):     &graphql.EnumValueConfig{Value: ct.EventTypeResourceDeletion},
		string(ct.EventTypeResourceAppDeletion):  &graphql.EnumValueConfig{Value: ct.EventTypeResourceAppDeletion},
		string(ct.EventTypeKey):                  &graphql.EnumValueConfig{Value: ct.EventTypeKey},
		string(ct.EventTypeKeyDeletion):          &graphql.EnumValueConfig{Value: ct.EventTypeKeyDeletion},
		string(ct.EventTypeRoute):                &graphql.EnumValueConfig{Value: ct.EventTypeRoute},
		string(ct.EventTypeRouteDeletion):        &graphql.EnumValueConfig{Value: ct.EventTypeRouteDeletion},
		string(ct.EventTypeDomainMigration):      &graphql.EnumValueConfig{Value: ct.EventTypeDomainMigration},
		string(ct.EventTypeClusterBackup):        &graphql.EnumValueConfig{Value: ct.EventTypeClusterBackup},
		string(ct.EventTypeAppGarbageCollection): &graphql.EnumValueConfig{Value: ct.EventTypeAppGarbageCollection},
	},
})

func wrapResolveFunc(fn func(*controllerAPI, graphql.ResolveParams) (interface{}, error)) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (interface{}, error) {
		api := p.Context.Value(apiContextKey).(*controllerAPI)
		return fn(api, p)
	}
}

func formationFieldResolveFunc(fn func(*controllerAPI, *ct.Formation) (interface{}, error)) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (interface{}, error) {
		api := p.Context.Value(apiContextKey).(*controllerAPI)
		if formation, ok := p.Source.(*ct.Formation); ok {
			return fn(api, formation)
		}
		return nil, nil
	}
}

func expandedFormationFieldResolveFunc(fn func(*controllerAPI, *ct.ExpandedFormation) (interface{}, error)) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (interface{}, error) {
		api := p.Context.Value(apiContextKey).(*controllerAPI)
		if formation, ok := p.Source.(*ct.ExpandedFormation); ok {
			return fn(api, formation)
		}
		return nil, nil
	}
}

func artifactFieldResolveFunc(fn func(*controllerAPI, *ct.Artifact) (interface{}, error)) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (interface{}, error) {
		api := p.Context.Value(apiContextKey).(*controllerAPI)
		if artifact, ok := p.Source.(*ct.Artifact); ok {
			return fn(api, artifact)
		}
		return nil, nil
	}
}

func releaseFieldResolveFunc(fn func(*controllerAPI, *ct.Release) (interface{}, error)) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (interface{}, error) {
		api := p.Context.Value(apiContextKey).(*controllerAPI)
		if release, ok := p.Source.(*ct.Release); ok {
			return fn(api, release)
		}
		return nil, nil
	}
}

func appFieldResolveFunc(fn func(*controllerAPI, *ct.App) (interface{}, error)) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (interface{}, error) {
		api := p.Context.Value(apiContextKey).(*controllerAPI)
		if app, ok := p.Source.(*ct.App); ok {
			return fn(api, app)
		}
		return nil, nil
	}
}

func deploymentFieldResolveFunc(fn func(*controllerAPI, *ct.Deployment) (interface{}, error)) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (interface{}, error) {
		api := p.Context.Value(apiContextKey).(*controllerAPI)
		if deployment, ok := p.Source.(*ct.Deployment); ok {
			return fn(api, deployment)
		}
		return nil, nil
	}
}

func jobFieldResolveFunc(fn func(*controllerAPI, *ct.Job) (interface{}, error)) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (interface{}, error) {
		api := p.Context.Value(apiContextKey).(*controllerAPI)
		if job, ok := p.Source.(*ct.Job); ok {
			return fn(api, job)
		}
		return nil, nil
	}
}

func providerFieldResolveFunc(fn func(*controllerAPI, *ct.Provider) (interface{}, error)) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (interface{}, error) {
		api := p.Context.Value(apiContextKey).(*controllerAPI)
		if provider, ok := p.Source.(*ct.Provider); ok {
			return fn(api, provider)
		}
		return nil, nil
	}
}

func resourceFieldResolveFunc(fn func(*controllerAPI, *ct.Resource) (interface{}, error)) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (interface{}, error) {
		api := p.Context.Value(apiContextKey).(*controllerAPI)
		if resource, ok := p.Source.(*ct.Resource); ok {
			return fn(api, resource)
		}
		return nil, nil
	}
}

func routeCertificateFieldResolveFunc(fn func(*controllerAPI, *router.Certificate) (interface{}, error)) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (interface{}, error) {
		api := p.Context.Value(apiContextKey).(*controllerAPI)
		if cert, ok := p.Source.(*router.Certificate); ok {
			return fn(api, cert)
		}
		return nil, nil
	}
}

func routeFieldResolveFunc(fn func(*controllerAPI, *router.Route) (interface{}, error)) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (interface{}, error) {
		api := p.Context.Value(apiContextKey).(*controllerAPI)
		if route, ok := p.Source.(*router.Route); ok {
			return fn(api, route)
		}
		return nil, nil
	}
}

func eventFieldResolveFunc(fn func(*controllerAPI, *ct.Event) (interface{}, error)) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (interface{}, error) {
		api := p.Context.Value(apiContextKey).(*controllerAPI)
		if event, ok := p.Source.(*ct.Event); ok {
			return fn(api, event)
		}
		return nil, nil
	}
}

func listArtifacts(api *controllerAPI, artifactIDs []string) ([]*ct.Artifact, error) {
	artifactMap, err := api.artifactRepo.ListIDs(artifactIDs...)
	if err != nil {
		return nil, err
	}
	artifacts := make([]*ct.Artifact, len(artifactMap))
	for i, id := range artifactIDs {
		artifacts[i] = artifactMap[id]
	}
	return artifacts, nil
}

func init() {
	formationObject := graphql.NewObject(graphql.ObjectConfig{
		Name: "Formation",
		Fields: graphql.Fields{
			"processes": &graphql.Field{
				Type:        processesObjectType,
				Description: "Processes",
				Resolve: formationFieldResolveFunc(func(_ *controllerAPI, f *ct.Formation) (interface{}, error) {
					return f.Processes, nil
				}),
			},
			"tags": &graphql.Field{
				Type:        tagsObjectType,
				Description: "Tags",
				Resolve: formationFieldResolveFunc(func(_ *controllerAPI, f *ct.Formation) (interface{}, error) {
					return f.Tags, nil
				}),
			},
			"created_at": &graphql.Field{
				Type:        graphqlTimeType,
				Description: "Time formation was created",
				Resolve: formationFieldResolveFunc(func(_ *controllerAPI, f *ct.Formation) (interface{}, error) {
					return f.CreatedAt, nil
				}),
			},
			"updated_at": &graphql.Field{
				Type:        graphqlTimeType,
				Description: "Time formation was last updated",
				Resolve: formationFieldResolveFunc(func(_ *controllerAPI, f *ct.Formation) (interface{}, error) {
					return f.UpdatedAt, nil
				}),
			},
		},
	})

	artifactObject := graphql.NewObject(graphql.ObjectConfig{
		Name: "Artifact",
		Fields: graphql.Fields{
			"id": &graphql.Field{
				Type:        graphql.String,
				Description: "UUID of artifact",
				Resolve: artifactFieldResolveFunc(func(_ *controllerAPI, artifact *ct.Artifact) (interface{}, error) {
					return artifact.ID, nil
				}),
			},
			"type": &graphql.Field{
				Type: graphql.NewEnum(graphql.EnumConfig{
					Name:        "ArtifactType",
					Description: "Type of artifact",
					Values: graphql.EnumValueConfigMap{
						string(host.ArtifactTypeDocker): &graphql.EnumValueConfig{
							Value:       host.ArtifactTypeDocker,
							Description: "Docker image",
						},
						string(host.ArtifactTypeFile): &graphql.EnumValueConfig{
							Value:       host.ArtifactTypeFile,
							Description: "Generic file",
						},
					},
				}),
				Resolve: artifactFieldResolveFunc(func(_ *controllerAPI, artifact *ct.Artifact) (interface{}, error) {
					return artifact.Type, nil
				}),
			},
			"uri": &graphql.Field{
				Type:        graphql.String,
				Description: "URI of artifact",
				Resolve: artifactFieldResolveFunc(func(_ *controllerAPI, artifact *ct.Artifact) (interface{}, error) {
					return artifact.URI, nil
				}),
			},
			"meta": &graphql.Field{
				Type:        metaObjectType,
				Description: "Meta for artifact",
				Resolve: artifactFieldResolveFunc(func(_ *controllerAPI, artifact *ct.Artifact) (interface{}, error) {
					return artifact.Meta, nil
				}),
			},
			"created_at": &graphql.Field{
				Type:        metaObjectType,
				Description: "Time artifact was created",
				Resolve: artifactFieldResolveFunc(func(_ *controllerAPI, artifact *ct.Artifact) (interface{}, error) {
					return artifact.CreatedAt, nil
				}),
			},
		},
	})

	releaseObject := graphql.NewObject(graphql.ObjectConfig{
		Name: "Release",
		Fields: graphql.Fields{
			"id": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.String),
				Description: "UUID of release",
				Resolve: releaseFieldResolveFunc(func(_ *controllerAPI, release *ct.Release) (interface{}, error) {
					return release.ID, nil
				}),
			},
			"artifacts": &graphql.Field{
				Type:        graphql.NewList(artifactObject),
				Description: "Artifacts for release",
				Resolve: releaseFieldResolveFunc(func(api *controllerAPI, release *ct.Release) (interface{}, error) {
					if len(release.ArtifactIDs) == 0 {
						return []*ct.Artifact{}, nil
					}
					return listArtifacts(api, release.ArtifactIDs)
				}),
			},
			"image_artifact": &graphql.Field{
				Type:        artifactObject,
				Description: "Image artifact for release",
				Resolve: releaseFieldResolveFunc(func(api *controllerAPI, release *ct.Release) (interface{}, error) {
					return api.artifactRepo.Get(release.ImageArtifactID())
				}),
			},
			"file_artifacts": &graphql.Field{
				Type:        graphql.NewList(artifactObject),
				Description: "File artifacts for release",
				Resolve: releaseFieldResolveFunc(func(api *controllerAPI, release *ct.Release) (interface{}, error) {
					return listArtifacts(api, release.FileArtifactIDs())
				}),
			},
			"env": &graphql.Field{
				Type:        metaObjectType,
				Description: "Env for release",
				Resolve: releaseFieldResolveFunc(func(_ *controllerAPI, release *ct.Release) (interface{}, error) {
					return release.Env, nil
				}),
			},
			"processes": &graphql.Field{
				Type:        processesObjectType,
				Description: "Processes included in deployment",
				Resolve: releaseFieldResolveFunc(func(_ *controllerAPI, r *ct.Release) (interface{}, error) {
					return r.Processes, nil
				}),
			},
			"meta": &graphql.Field{
				Type:        metaObjectType,
				Description: "Metadata for release",
				Resolve: releaseFieldResolveFunc(func(_ *controllerAPI, release *ct.Release) (interface{}, error) {
					return release.Meta, nil
				}),
			},
			"created_at": &graphql.Field{
				Type:        graphqlTimeType,
				Description: "Time formation was created",
				Resolve: releaseFieldResolveFunc(func(_ *controllerAPI, release *ct.Release) (interface{}, error) {
					return release.CreatedAt, nil
				}),
			},
		},
	})

	appObject := graphql.NewObject(graphql.ObjectConfig{
		Name: "App",
		Fields: graphql.Fields{
			"id": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.String),
				Description: "UUID of app",
				Resolve: appFieldResolveFunc(func(_ *controllerAPI, app *ct.App) (interface{}, error) {
					return app.ID, nil
				}),
			},
			"name": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.String),
				Description: "Name of app",
				Resolve: appFieldResolveFunc(func(_ *controllerAPI, app *ct.App) (interface{}, error) {
					return app.Name, nil
				}),
			},
			"meta": &graphql.Field{
				Type:        metaObjectType,
				Description: "Metadata for app",
				Resolve: appFieldResolveFunc(func(_ *controllerAPI, app *ct.App) (interface{}, error) {
					return app.Meta, nil
				}),
			},
			"strategy": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.String),
				Description: "Deployment strategy for app",
				Resolve: appFieldResolveFunc(func(_ *controllerAPI, app *ct.App) (interface{}, error) {
					return app.Strategy, nil
				}),
			},
			"deploy_timeout": &graphql.Field{
				Type:        graphql.Int,
				Description: "Deploy timeout in seconds",
				Resolve: appFieldResolveFunc(func(_ *controllerAPI, app *ct.App) (interface{}, error) {
					return app.DeployTimeout, nil
				}),
			},
			"created_at": &graphql.Field{
				Type:        graphqlTimeType,
				Description: "Time app was created",
				Resolve: appFieldResolveFunc(func(_ *controllerAPI, app *ct.App) (interface{}, error) {
					return app.CreatedAt, nil
				}),
			},
			"updated_at": &graphql.Field{
				Type:        graphqlTimeType,
				Description: "Time app was last updated",
				Resolve: appFieldResolveFunc(func(_ *controllerAPI, app *ct.App) (interface{}, error) {
					return app.UpdatedAt, nil
				}),
			},
			"current_release": &graphql.Field{
				Type:        releaseObject,
				Description: "Current release for app",
				Resolve: appFieldResolveFunc(func(api *controllerAPI, app *ct.App) (interface{}, error) {
					release, err := api.appRepo.GetRelease(app.ID)
					if err == ErrNotFound {
						// not all apps have a release
						return nil, nil
					}
					return release, err
				}),
			},
			"releases": &graphql.Field{
				Type:        graphql.NewList(releaseObject),
				Description: "Releases for app",
				Resolve: appFieldResolveFunc(func(api *controllerAPI, app *ct.App) (interface{}, error) {
					return api.releaseRepo.AppList(app.ID)
				}),
			},
			"formations": &graphql.Field{
				Type:        graphql.NewList(formationObject),
				Description: "Formations for app",
				Resolve: appFieldResolveFunc(func(api *controllerAPI, app *ct.App) (interface{}, error) {
					return api.formationRepo.List(app.ID)
				}),
			},
		},
	})

	deploymentObject := graphql.NewObject(graphql.ObjectConfig{
		Name: "Deployment",
		Fields: graphql.Fields{
			"id": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.String),
				Description: "UUID of app",
				Resolve: deploymentFieldResolveFunc(func(_ *controllerAPI, d *ct.Deployment) (interface{}, error) {
					return d.ID, nil
				}),
			},
			"app": &graphql.Field{
				Type:        appObject,
				Description: "App deployment belongs to",
				Resolve: deploymentFieldResolveFunc(func(api *controllerAPI, d *ct.Deployment) (interface{}, error) {
					app, err := api.appRepo.Get(d.AppID)
					if err == ErrNotFound {
						return nil, nil
					}
					return app, err
				}),
			},
			"old_release": &graphql.Field{
				Type:        releaseObject,
				Description: "Old release",
				Resolve: deploymentFieldResolveFunc(func(api *controllerAPI, d *ct.Deployment) (interface{}, error) {
					r, err := api.releaseRepo.Get(d.OldReleaseID)
					if err == ErrNotFound {
						return nil, nil
					}
					return r, err
				}),
			},
			"new_release": &graphql.Field{
				Type:        releaseObject,
				Description: "New release",
				Resolve: deploymentFieldResolveFunc(func(api *controllerAPI, d *ct.Deployment) (interface{}, error) {
					r, err := api.releaseRepo.Get(d.NewReleaseID)
					if err == ErrNotFound {
						return nil, nil
					}
					return r, err
				}),
			},
			"strategy": &graphql.Field{
				Type:        graphql.String,
				Description: "Deployment stategy",
				Resolve: deploymentFieldResolveFunc(func(_ *controllerAPI, d *ct.Deployment) (interface{}, error) {
					return d.Strategy, nil
				}),
			},
			"status": &graphql.Field{
				Type:        graphql.String,
				Description: "Status of deployment",
				Resolve: deploymentFieldResolveFunc(func(_ *controllerAPI, d *ct.Deployment) (interface{}, error) {
					return d.Status, nil
				}),
			},
			"deploy_timeout": &graphql.Field{
				Type:        graphql.Int,
				Description: "Time in seconds before the deployment times out",
				Resolve: deploymentFieldResolveFunc(func(_ *controllerAPI, d *ct.Deployment) (interface{}, error) {
					return d.Status, nil
				}),
			},
			"processes": &graphql.Field{
				Type:        processesObjectType,
				Description: "Processes included in deployment",
				Resolve: deploymentFieldResolveFunc(func(_ *controllerAPI, d *ct.Deployment) (interface{}, error) {
					return d.Processes, nil
				}),
			},
			"created_at": &graphql.Field{
				Type:        graphqlTimeType,
				Description: "Time deployment was created",
				Resolve: deploymentFieldResolveFunc(func(_ *controllerAPI, d *ct.Deployment) (interface{}, error) {
					return d.CreatedAt, nil
				}),
			},
			"finished_at": &graphql.Field{
				Type:        graphqlTimeType,
				Description: "Time deployment finished",
				Resolve: deploymentFieldResolveFunc(func(_ *controllerAPI, d *ct.Deployment) (interface{}, error) {
					return d.FinishedAt, nil
				}),
			},
		},
	})

	jobObject := graphql.NewObject(graphql.ObjectConfig{
		Name: "Job",
		Fields: graphql.Fields{
			"id": &graphql.Field{
				Type:        graphql.String,
				Description: "Full cluster ID of job",
				Resolve: jobFieldResolveFunc(func(_ *controllerAPI, job *ct.Job) (interface{}, error) {
					return job.ID, nil
				}),
			},
			"uuid": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.String),
				Description: "UUID of job",
				Resolve: jobFieldResolveFunc(func(_ *controllerAPI, job *ct.Job) (interface{}, error) {
					return job.UUID, nil
				}),
			},
			"host_id": &graphql.Field{
				Type:        graphql.String,
				Description: "Host ID of job",
				Resolve: jobFieldResolveFunc(func(_ *controllerAPI, job *ct.Job) (interface{}, error) {
					return job.HostID, nil
				}),
			},
			"app": &graphql.Field{
				Type:        appObject,
				Description: "App job belongs to",
				Resolve: jobFieldResolveFunc(func(api *controllerAPI, job *ct.Job) (interface{}, error) {
					return api.appRepo.Get(job.AppID)
				}),
			},
			"release": &graphql.Field{
				Type:        releaseObject,
				Description: "Release job belongs to",
				Resolve: jobFieldResolveFunc(func(api *controllerAPI, job *ct.Job) (interface{}, error) {
					r, err := api.releaseRepo.Get(job.ReleaseID)
					if err == ErrNotFound {
						return nil, nil
					}
					return r, err
				}),
			},
			"type": &graphql.Field{
				Type:        graphql.String,
				Description: "Type of job",
				Resolve: jobFieldResolveFunc(func(_ *controllerAPI, job *ct.Job) (interface{}, error) {
					return job.Type, nil
				}),
			},
			"state": &graphql.Field{
				Type: graphql.NewEnum(graphql.EnumConfig{
					Name:        "JobState",
					Description: "State of job",
					Values: graphql.EnumValueConfigMap{
						string(ct.JobStatePending): &graphql.EnumValueConfig{
							Value: ct.JobStatePending,
						},
						string(ct.JobStateStarting): &graphql.EnumValueConfig{
							Value: ct.JobStateStarting,
						},
						string(ct.JobStateUp): &graphql.EnumValueConfig{
							Value: ct.JobStateUp,
						},
						string(ct.JobStateStopping): &graphql.EnumValueConfig{
							Value: ct.JobStateStopping,
						},
						string(ct.JobStateDown): &graphql.EnumValueConfig{
							Value: ct.JobStateDown,
						},
						string(ct.JobStateCrashed): &graphql.EnumValueConfig{
							Value:             ct.JobStateCrashed,
							DeprecationReason: "No longer a valid job state",
						},
						string(ct.JobStateFailed): &graphql.EnumValueConfig{
							Value:             ct.JobStateFailed,
							DeprecationReason: "No longer a valid job state",
						},
					},
				}),
				Description: "State of job",
				Resolve: jobFieldResolveFunc(func(_ *controllerAPI, job *ct.Job) (interface{}, error) {
					return job.State, nil
				}),
			},
			"args": &graphql.Field{
				Type:        graphql.NewList(graphql.String),
				Description: "Args of job",
				Resolve: jobFieldResolveFunc(func(_ *controllerAPI, job *ct.Job) (interface{}, error) {
					return job.Args, nil
				}),
			},
			"meta": &graphql.Field{
				Type:        metaObjectType,
				Description: "Cmd of job",
				Resolve: jobFieldResolveFunc(func(_ *controllerAPI, job *ct.Job) (interface{}, error) {
					return job.Meta, nil
				}),
			},
			"exit_status": &graphql.Field{
				Type:        graphql.Int,
				Description: "Exit status of job",
				Resolve: jobFieldResolveFunc(func(_ *controllerAPI, job *ct.Job) (interface{}, error) {
					return job.ExitStatus, nil
				}),
			},
			"host_error": &graphql.Field{
				Type:        graphql.String,
				Description: "Host error",
				Resolve: jobFieldResolveFunc(func(_ *controllerAPI, job *ct.Job) (interface{}, error) {
					return job.HostError, nil
				}),
			},
			"run_at": &graphql.Field{
				Type:        graphqlTimeType,
				Description: "Time job should run at",
				Resolve: jobFieldResolveFunc(func(_ *controllerAPI, job *ct.Job) (interface{}, error) {
					return job.RunAt, nil
				}),
			},
			"restarts": &graphql.Field{
				Type:        graphql.Int,
				Description: "Number of times job has restarted",
				Resolve: jobFieldResolveFunc(func(_ *controllerAPI, job *ct.Job) (interface{}, error) {
					return job.Restarts, nil
				}),
			},
			"created_at": &graphql.Field{
				Type:        graphqlTimeType,
				Description: "Time job was created",
				Resolve: jobFieldResolveFunc(func(_ *controllerAPI, job *ct.Job) (interface{}, error) {
					return job.CreatedAt, nil
				}),
			},
			"updated_at": &graphql.Field{
				Type:        graphqlTimeType,
				Description: "Time job was last updated",
				Resolve: jobFieldResolveFunc(func(_ *controllerAPI, job *ct.Job) (interface{}, error) {
					return job.UpdatedAt, nil
				}),
			},
		},
	})

	providerObject := graphql.NewObject(graphql.ObjectConfig{
		Name: "Provider",
		Fields: graphql.Fields{
			"id": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.String),
				Description: "UUID of provider",
				Resolve: providerFieldResolveFunc(func(_ *controllerAPI, p *ct.Provider) (interface{}, error) {
					return p.ID, nil
				}),
			},
			"url": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.String),
				Description: "URL of provider",
				Resolve: providerFieldResolveFunc(func(_ *controllerAPI, p *ct.Provider) (interface{}, error) {
					return p.URL, nil
				}),
			},
			"name": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.String),
				Description: "Name of provider",
				Resolve: providerFieldResolveFunc(func(_ *controllerAPI, p *ct.Provider) (interface{}, error) {
					return p.Name, nil
				}),
			},
			"created_at": &graphql.Field{
				Type:        graphqlTimeType,
				Description: "Time provider was created",
				Resolve: providerFieldResolveFunc(func(_ *controllerAPI, p *ct.Provider) (interface{}, error) {
					return p.CreatedAt, nil
				}),
			},
			"updated_at": &graphql.Field{
				Type:        graphqlTimeType,
				Description: "Time provider was last updated",
				Resolve: providerFieldResolveFunc(func(_ *controllerAPI, p *ct.Provider) (interface{}, error) {
					return p.UpdatedAt, nil
				}),
			},
		},
	})

	resourceObject := graphql.NewObject(graphql.ObjectConfig{
		Name: "Resource",
		Fields: graphql.Fields{
			"id": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.String),
				Description: "UUID of resource",
				Resolve: resourceFieldResolveFunc(func(_ *controllerAPI, r *ct.Resource) (interface{}, error) {
					return r.ID, nil
				}),
			},
			"provider": &graphql.Field{
				Type:        providerObject,
				Description: "Provider of resource",
				Resolve: resourceFieldResolveFunc(func(api *controllerAPI, r *ct.Resource) (interface{}, error) {
					return api.providerRepo.Get(r.ProviderID)
				}),
			},
			"external_id": &graphql.Field{
				Type:        graphql.String,
				Description: "External ID of resource",
				Resolve: resourceFieldResolveFunc(func(_ *controllerAPI, r *ct.Resource) (interface{}, error) {
					return r.ExternalID, nil
				}),
			},
			"env": &graphql.Field{
				Type:        envObjectType,
				Description: "Env of resource",
				Resolve: resourceFieldResolveFunc(func(_ *controllerAPI, r *ct.Resource) (interface{}, error) {
					return r.Env, nil
				}),
			},
			"apps": &graphql.Field{
				Type:        graphql.NewList(appObject),
				Description: "Apps associated with resource",
				Resolve: resourceFieldResolveFunc(func(api *controllerAPI, r *ct.Resource) (interface{}, error) {
					return api.appRepo.ListIDs(r.Apps...)
				}),
			},
			"created_at": &graphql.Field{
				Type:        graphqlTimeType,
				Description: "Time resource was created at",
				Resolve: resourceFieldResolveFunc(func(_ *controllerAPI, r *ct.Resource) (interface{}, error) {
					return r.CreatedAt, nil
				}),
			},
		},
	})

	routeCertificateObject := graphql.NewObject(graphql.ObjectConfig{
		Name: "RouteCertificate",
		Fields: graphql.Fields{
			"id": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.String),
				Description: "UUID of certificate",
				Resolve: routeCertificateFieldResolveFunc(func(_ *controllerAPI, c *router.Certificate) (interface{}, error) {
					return c.ID, nil
				}),
			},
			"cert": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.String),
				Description: "TLS certificate",
				Resolve: routeCertificateFieldResolveFunc(func(_ *controllerAPI, c *router.Certificate) (interface{}, error) {
					return c.Cert, nil
				}),
			},
			"key": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.String),
				Description: "TLS private key",
				Resolve: routeCertificateFieldResolveFunc(func(_ *controllerAPI, c *router.Certificate) (interface{}, error) {
					return c.Key, nil
				}),
			},
			"created_at": &graphql.Field{
				Type:        graphqlTimeType,
				Description: "Time certificate was created at",
				Resolve: routeCertificateFieldResolveFunc(func(_ *controllerAPI, c *router.Certificate) (interface{}, error) {
					return c.CreatedAt, nil
				}),
			},
			"updated_at": &graphql.Field{
				Type:        graphqlTimeType,
				Description: "Time certificate was last updated",
				Resolve: routeCertificateFieldResolveFunc(func(_ *controllerAPI, c *router.Certificate) (interface{}, error) {
					return c.UpdatedAt, nil
				}),
			},
		},
	})

	routeObject := graphql.NewObject(graphql.ObjectConfig{
		Name: "Route",
		Fields: graphql.Fields{
			"type": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.String),
				Description: "Type of route",
				Resolve: routeFieldResolveFunc(func(_ *controllerAPI, r *router.Route) (interface{}, error) {
					return r.Type, nil
				}),
			},
			"id": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.String),
				Description: "UUID of route",
				Resolve: routeFieldResolveFunc(func(_ *controllerAPI, r *router.Route) (interface{}, error) {
					return r.ID, nil
				}),
			},
			"parent_ref": &graphql.Field{
				Type:        graphql.String,
				Description: "External opaque ID",
				Resolve: routeFieldResolveFunc(func(_ *controllerAPI, r *router.Route) (interface{}, error) {
					return r.ParentRef, nil
				}),
			},
			"service": &graphql.Field{
				Type:        graphql.String,
				Description: "ID of the service",
				Resolve: routeFieldResolveFunc(func(_ *controllerAPI, r *router.Route) (interface{}, error) {
					return r.Service, nil
				}),
			},
			"leader": &graphql.Field{
				Type:        graphql.Boolean,
				Description: "Route traffic to the only to the leader when true",
				Resolve: routeFieldResolveFunc(func(_ *controllerAPI, r *router.Route) (interface{}, error) {
					return r.Leader, nil
				}),
			},
			"created_at": &graphql.Field{
				Type:        graphqlTimeType,
				Description: "Time route was created",
				Resolve: routeFieldResolveFunc(func(_ *controllerAPI, r *router.Route) (interface{}, error) {
					return r.CreatedAt, nil
				}),
			},
			"updated_at": &graphql.Field{
				Type:        graphqlTimeType,
				Description: "Time route was last updated",
				Resolve: routeFieldResolveFunc(func(_ *controllerAPI, r *router.Route) (interface{}, error) {
					return r.UpdatedAt, nil
				}),
			},
			"domain": &graphql.Field{
				Type:        graphql.String,
				Description: "Domain name of route (HTTP routes only)",
				Resolve: routeFieldResolveFunc(func(_ *controllerAPI, r *router.Route) (interface{}, error) {
					return r.Domain, nil
				}),
			},
			"sticky": &graphql.Field{
				Type:        graphql.Boolean,
				Description: "Use sticky sessions for route when true (HTTP routes only)",
				Resolve: routeFieldResolveFunc(func(_ *controllerAPI, r *router.Route) (interface{}, error) {
					return r.Sticky, nil
				}),
			},
			"path": &graphql.Field{
				Type:        graphql.String,
				Description: "Prefix to route to this service.",
				Resolve: routeFieldResolveFunc(func(_ *controllerAPI, r *router.Route) (interface{}, error) {
					return r.Path, nil
				}),
			},
			"port": &graphql.Field{
				Type:        graphql.Int,
				Description: "TPC port to listen on (TCP routes only)",
				Resolve: routeFieldResolveFunc(func(_ *controllerAPI, r *router.Route) (interface{}, error) {
					return r.Port, nil
				}),
			},
			"certificate": &graphql.Field{
				Type:        routeCertificateObject,
				Description: "TLS certificate for route",
				Resolve: routeFieldResolveFunc(func(_ *controllerAPI, r *router.Route) (interface{}, error) {
					if r.Certificate == nil {
						return nil, nil
					}
					return r.Certificate, nil
				}),
			},
			"app": &graphql.Field{
				Type:        appObject,
				Description: "App route belongs to",
				Resolve: routeFieldResolveFunc(func(api *controllerAPI, r *router.Route) (interface{}, error) {
					if strings.HasPrefix(r.ParentRef, ct.RouteParentRefPrefix) {
						appID := strings.TrimPrefix(r.ParentRef, ct.RouteParentRefPrefix)
						return api.appRepo.Get(appID)
					}
					return nil, nil
				}),
			},
		},
	})

	routeCertificateObject.AddFieldConfig("routes", &graphql.Field{
		Type:        graphql.NewList(routeObject),
		Description: "Routes using certificate",
		Resolve: routeCertificateFieldResolveFunc(func(api *controllerAPI, c *router.Certificate) (interface{}, error) {
			return api.routerc.ListCertRoutes(c.ID)
		}),
	})

	eventObject := graphql.NewObject(graphql.ObjectConfig{
		Name: "Event",
		Fields: graphql.Fields{
			"id": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.Int),
				Description: "ID of event",
				Resolve: eventFieldResolveFunc(func(_ *controllerAPI, event *ct.Event) (interface{}, error) {
					return event.ID, nil
				}),
			},
			"object_type": &graphql.Field{
				Type:        eventObjectTypeEnum,
				Description: "Type of event",
				Resolve: eventFieldResolveFunc(func(_ *controllerAPI, event *ct.Event) (interface{}, error) {
					return event.ObjectType, nil
				}),
			},
			"object_id": &graphql.Field{
				Type:        graphql.String,
				Description: "UUID of object",
				Resolve: eventFieldResolveFunc(func(_ *controllerAPI, event *ct.Event) (interface{}, error) {
					return event.ObjectID, nil
				}),
			},
			"data": &graphql.Field{
				Type:        eventDataObjectType,
				Description: "Event data",
				Resolve: eventFieldResolveFunc(func(_ *controllerAPI, event *ct.Event) (interface{}, error) {
					var data interface{}
					return data, json.Unmarshal(event.Data, &data)
				}),
			},
			"created_at": &graphql.Field{
				Type:        graphqlTimeType,
				Description: "Time event was created",
				Resolve: eventFieldResolveFunc(func(_ *controllerAPI, event *ct.Event) (interface{}, error) {
					return event.CreatedAt, nil
				}),
			},
			"app": &graphql.Field{
				Type:        appObject,
				Description: "App event belongs to",
				Resolve: eventFieldResolveFunc(func(api *controllerAPI, event *ct.Event) (interface{}, error) {
					if event.AppID == "" {
						return nil, nil
					}
					return api.appRepo.Get(event.AppID)
				}),
			},
		},
	})

	expandedFormationObject := graphql.NewObject(graphql.ObjectConfig{
		Name: "ExpandedFormation",
		Fields: graphql.Fields{
			"app": &graphql.Field{
				Type:        appObject,
				Description: "App formation belongs to",
				Resolve: expandedFormationFieldResolveFunc(func(api *controllerAPI, f *ct.ExpandedFormation) (interface{}, error) {
					return api.appRepo.Get(f.App.ID)
				}),
			},
			"release": &graphql.Field{
				Type:        releaseObject,
				Description: "Release formation belongs to",
				Resolve: expandedFormationFieldResolveFunc(func(api *controllerAPI, f *ct.ExpandedFormation) (interface{}, error) {
					return api.releaseRepo.Get(f.Release.ID)
				}),
			},
			"image_artifact": &graphql.Field{
				Type:        artifactObject,
				Description: "Image artifact",
				Resolve: expandedFormationFieldResolveFunc(func(api *controllerAPI, f *ct.ExpandedFormation) (interface{}, error) {
					return f.ImageArtifact, nil
				}),
			},
			"file_artifacts": &graphql.Field{
				Type:        graphql.NewList(artifactObject),
				Description: "File artifacts",
				Resolve: expandedFormationFieldResolveFunc(func(api *controllerAPI, f *ct.ExpandedFormation) (interface{}, error) {
					return f.FileArtifacts, nil
				}),
			},
			"processes": &graphql.Field{
				Type:        processesObjectType,
				Description: "Processes",
				Resolve: expandedFormationFieldResolveFunc(func(_ *controllerAPI, f *ct.ExpandedFormation) (interface{}, error) {
					return f.Processes, nil
				}),
			},
			"tags": &graphql.Field{
				Type:        tagsObjectType,
				Description: "Tags",
				Resolve: expandedFormationFieldResolveFunc(func(_ *controllerAPI, f *ct.ExpandedFormation) (interface{}, error) {
					return f.Tags, nil
				}),
			},
			"updated_at": &graphql.Field{
				Type:        graphqlTimeType,
				Description: "Time formation was last updated",
				Resolve: expandedFormationFieldResolveFunc(func(_ *controllerAPI, f *ct.ExpandedFormation) (interface{}, error) {
					return f.UpdatedAt, nil
				}),
			},
		},
	})

	formationObject.AddFieldConfig("app", &graphql.Field{
		Type:        appObject,
		Description: "App formation belongs to",
		Resolve: formationFieldResolveFunc(func(api *controllerAPI, f *ct.Formation) (interface{}, error) {
			return api.appRepo.Get(f.AppID)
		}),
	})
	formationObject.AddFieldConfig("release", &graphql.Field{
		Type:        releaseObject,
		Description: "Release formation belongs to",
		Resolve: formationFieldResolveFunc(func(api *controllerAPI, f *ct.Formation) (interface{}, error) {
			return api.releaseRepo.Get(f.ReleaseID)
		}),
	})

	appObject.AddFieldConfig("resources", &graphql.Field{
		Type:        graphql.NewList(resourceObject),
		Description: "Resources for app",
		Resolve: appFieldResolveFunc(func(api *controllerAPI, app *ct.App) (interface{}, error) {
			return api.resourceRepo.AppList(app.ID)
		}),
	})
	appObject.AddFieldConfig("deployments", &graphql.Field{
		Type:        graphql.NewList(deploymentObject),
		Description: "Deployments for app",
		Resolve: appFieldResolveFunc(func(api *controllerAPI, app *ct.App) (interface{}, error) {
			return api.deploymentRepo.List(app.ID)
		}),
	})
	appObject.AddFieldConfig("jobs", &graphql.Field{
		Type:        graphql.NewList(jobObject),
		Description: "Jobs for app",
		Resolve: appFieldResolveFunc(func(api *controllerAPI, app *ct.App) (interface{}, error) {
			return api.jobRepo.List(app.ID)
		}),
	})
	appObject.AddFieldConfig("routes", &graphql.Field{
		Type:        graphql.NewList(routeObject),
		Description: "Routes for app",
		Resolve: appFieldResolveFunc(func(api *controllerAPI, app *ct.App) (interface{}, error) {
			return api.routerc.ListRoutes(routeParentRef(app.ID))
		}),
	})
	appObject.AddFieldConfig("events", &graphql.Field{
		Type:        graphql.NewList(eventObject),
		Description: "Events for app",
		Args: graphql.FieldConfigArgument{
			"object_types": &graphql.ArgumentConfig{
				Description: "Filters events by object types",
				Type:        graphql.NewList(graphql.String),
			},
			"object_id": &graphql.ArgumentConfig{
				Description: "Filters events by object id",
				Type:        graphql.String,
			},
			"app_id": &graphql.ArgumentConfig{
				Description: "Filteres events by app id",
				Type:        graphql.String,
			},
			"count": &graphql.ArgumentConfig{
				Description: "Number of events to return",
				Type:        graphql.Int,
			},
			"before_id": &graphql.ArgumentConfig{
				Description: "Return only events before specified event id",
				Type:        graphql.Int,
			},
			"since_id": &graphql.ArgumentConfig{
				Description: "Return only events after specified event id",
				Type:        graphql.Int,
			},
		},
		Resolve: wrapResolveFunc(func(api *controllerAPI, p graphql.ResolveParams) (interface{}, error) {
			app, ok := p.Source.(*ct.App)
			if !ok {
				return nil, nil
			}
			var beforeID *int64
			if i, ok := p.Args["before_id"]; ok {
				if id, ok := i.(int); ok {
					id64 := int64(id)
					beforeID = &id64
				}
			}
			var sinceID *int64
			if i, ok := p.Args["since_id"]; ok {
				if id, ok := i.(int); ok {
					id64 := int64(id)
					sinceID = &id64
				}
			}
			var count int
			if i, ok := p.Args["count"]; ok {
				if n, ok := i.(int); ok {
					count = n
				}
			}
			var objectTypes []string
			if i, ok := p.Args["object_types"]; ok {
				for _, v := range i.([]interface{}) {
					objectTypes = append(objectTypes, v.(string))
				}
			}
			var objectID string
			if i, ok := p.Args["object_id"]; ok {
				objectID = i.(string)
			}
			return api.eventRepo.ListEvents(app.ID, objectTypes, objectID, beforeID, sinceID, count)
		}),
	})

	providerObject.AddFieldConfig("resources", &graphql.Field{
		Type:        graphql.NewList(resourceObject),
		Description: "Resources for provider",
		Resolve: providerFieldResolveFunc(func(api *controllerAPI, p *ct.Provider) (interface{}, error) {
			return api.resourceRepo.ProviderList(p.ID)
		}),
	})

	var err error
	graphqlSchema, err = graphql.NewSchema(graphql.SchemaConfig{
		Query: graphql.NewObject(graphql.ObjectConfig{
			Name: "RootQuery",
			Fields: graphql.Fields{
				"app": &graphql.Field{
					Type: appObject,
					Args: graphql.FieldConfigArgument{
						"id": &graphql.ArgumentConfig{
							Description: "UUID or name of app",
							Type:        graphql.NewNonNull(graphql.String),
						},
					},
					Resolve: wrapResolveFunc(func(api *controllerAPI, p graphql.ResolveParams) (interface{}, error) {
						return api.appRepo.Get(p.Args["id"].(string))
					}),
				},
				"apps": &graphql.Field{
					Type: graphql.NewList(appObject),
					Resolve: wrapResolveFunc(func(api *controllerAPI, p graphql.ResolveParams) (interface{}, error) {
						return api.appRepo.List()
					}),
				},
				"artifact": &graphql.Field{
					Type: artifactObject,
					Args: graphql.FieldConfigArgument{
						"id": &graphql.ArgumentConfig{
							Description: "UUID of artifact",
							Type:        graphql.NewNonNull(graphql.String),
						},
					},
					Resolve: wrapResolveFunc(func(api *controllerAPI, p graphql.ResolveParams) (interface{}, error) {
						return api.artifactRepo.Get(p.Args["id"].(string))
					}),
				},
				"artifacts": &graphql.Field{
					Type: graphql.NewList(artifactObject),
					Resolve: wrapResolveFunc(func(api *controllerAPI, p graphql.ResolveParams) (interface{}, error) {
						return api.artifactRepo.List()
					}),
				},
				"release": &graphql.Field{
					Type: releaseObject,
					Args: graphql.FieldConfigArgument{
						"id": &graphql.ArgumentConfig{
							Description: "UUID of release",
							Type:        graphql.NewNonNull(graphql.String),
						},
					},
					Resolve: wrapResolveFunc(func(api *controllerAPI, p graphql.ResolveParams) (interface{}, error) {
						return api.releaseRepo.Get(p.Args["id"].(string))
					}),
				},
				"releases": &graphql.Field{
					Type: graphql.NewList(releaseObject),
					Resolve: wrapResolveFunc(func(api *controllerAPI, p graphql.ResolveParams) (interface{}, error) {
						return api.releaseRepo.List()
					}),
				},
				"formation": &graphql.Field{
					Type: formationObject,
					Args: graphql.FieldConfigArgument{
						"app": &graphql.ArgumentConfig{
							Type: graphql.NewNonNull(graphql.String),
						},
						"release": &graphql.ArgumentConfig{
							Type: graphql.NewNonNull(graphql.String),
						},
					},
					Resolve: wrapResolveFunc(func(api *controllerAPI, p graphql.ResolveParams) (interface{}, error) {
						app, err := api.appRepo.Get(p.Args["app"].(string))
						if err != nil {
							return nil, err
						}
						return api.formationRepo.Get(app.(*ct.App).ID, p.Args["release"].(string))
					}),
				},
				"active_formations": &graphql.Field{
					Type: graphql.NewList(expandedFormationObject),
					Resolve: wrapResolveFunc(func(api *controllerAPI, p graphql.ResolveParams) (interface{}, error) {
						return api.formationRepo.ListActive()
					}),
				},
				"expanded_formation": &graphql.Field{
					Type: expandedFormationObject,
					Args: graphql.FieldConfigArgument{
						"app": &graphql.ArgumentConfig{
							Type: graphql.NewNonNull(graphql.String),
						},
						"release": &graphql.ArgumentConfig{
							Type: graphql.NewNonNull(graphql.String),
						},
					},
					Resolve: wrapResolveFunc(func(api *controllerAPI, p graphql.ResolveParams) (interface{}, error) {
						app, err := api.appRepo.Get(p.Args["app"].(string))
						if err != nil {
							return nil, err
						}
						return api.formationRepo.GetExpanded(app.(*ct.App).ID, p.Args["release"].(string), false)
					}),
				},
				"deployment": &graphql.Field{
					Type: deploymentObject,
					Args: graphql.FieldConfigArgument{
						"id": &graphql.ArgumentConfig{
							Description: "UUID of deployment",
							Type:        graphql.NewNonNull(graphql.String),
						},
					},
					Resolve: wrapResolveFunc(func(api *controllerAPI, p graphql.ResolveParams) (interface{}, error) {
						return api.deploymentRepo.Get(p.Args["id"].(string))
					}),
				},
				"job": &graphql.Field{
					Type: jobObject,
					Args: graphql.FieldConfigArgument{
						"id": &graphql.ArgumentConfig{
							Description: "ID or UUID of job",
							Type:        graphql.NewNonNull(graphql.String),
						},
						"app": &graphql.ArgumentConfig{
							Description: "ID of app job belongs to",
							Type:        graphql.NewNonNull(graphql.String),
						},
					},
					Resolve: wrapResolveFunc(func(api *controllerAPI, p graphql.ResolveParams) (interface{}, error) {
						_, err := api.appRepo.Get(p.Args["app"].(string))
						if err != nil {
							return nil, err
						}
						return api.jobRepo.Get(p.Args["id"].(string))
					}),
				},
				"active_jobs": &graphql.Field{
					Type: graphql.NewList(jobObject),
					Resolve: wrapResolveFunc(func(api *controllerAPI, _ graphql.ResolveParams) (interface{}, error) {
						return api.jobRepo.ListActive()
					}),
				},
				"provider": &graphql.Field{
					Type: providerObject,
					Args: graphql.FieldConfigArgument{
						"id": &graphql.ArgumentConfig{
							Description: "UUID of provider",
							Type:        graphql.NewNonNull(graphql.String),
						},
					},
					Resolve: wrapResolveFunc(func(api *controllerAPI, p graphql.ResolveParams) (interface{}, error) {
						return api.providerRepo.Get(p.Args["id"].(string))
					}),
				},
				"providers": &graphql.Field{
					Type: graphql.NewList(providerObject),
					Resolve: wrapResolveFunc(func(api *controllerAPI, p graphql.ResolveParams) (interface{}, error) {
						return api.providerRepo.List()
					}),
				},
				"resource": &graphql.Field{
					Type: resourceObject,
					Args: graphql.FieldConfigArgument{
						"provider": &graphql.ArgumentConfig{
							Description: "UUID of provider",
							Type:        graphql.NewNonNull(graphql.String),
						},
						"id": &graphql.ArgumentConfig{
							Description: "UUID of resource",
							Type:        graphql.NewNonNull(graphql.String),
						},
					},
					Resolve: wrapResolveFunc(func(api *controllerAPI, p graphql.ResolveParams) (interface{}, error) {
						_, err := api.providerRepo.Get(p.Args["provider"].(string))
						if err != nil {
							return nil, err
						}
						return api.resourceRepo.Get(p.Args["id"].(string))
					}),
				},
				"resources": &graphql.Field{
					Type: graphql.NewList(resourceObject),
					Resolve: wrapResolveFunc(func(api *controllerAPI, p graphql.ResolveParams) (interface{}, error) {
						return api.resourceRepo.List()
					}),
				},
				"route": &graphql.Field{
					Type: routeObject,
					Args: graphql.FieldConfigArgument{
						"app": &graphql.ArgumentConfig{
							Description: "UUID of app",
							Type:        graphql.NewNonNull(graphql.String),
						},
						"id": &graphql.ArgumentConfig{
							Description: "UUID of route",
							Type:        graphql.NewNonNull(graphql.String),
						},
					},
					Resolve: wrapResolveFunc(func(api *controllerAPI, p graphql.ResolveParams) (interface{}, error) {
						parts := strings.SplitN(p.Args["id"].(string), "/", 2)
						return api.getRoute(p.Args["app"].(string), parts[0], parts[1])
					}),
				},
				"event": &graphql.Field{
					Type: eventObject,
					Args: graphql.FieldConfigArgument{
						"id": &graphql.ArgumentConfig{
							Description: "UUID of event",
							Type:        graphql.NewNonNull(graphql.Int),
						},
					},
					Resolve: wrapResolveFunc(func(api *controllerAPI, p graphql.ResolveParams) (interface{}, error) {
						return api.eventRepo.GetEvent(int64(p.Args["id"].(int)))
					}),
				},
				"events": &graphql.Field{
					Type: graphql.NewList(eventObject),
					Args: graphql.FieldConfigArgument{
						"object_types": &graphql.ArgumentConfig{
							Description: "Filters events by object types",
							Type:        graphql.NewList(graphql.String),
						},
						"object_id": &graphql.ArgumentConfig{
							Description: "Filters events by object id",
							Type:        graphql.String,
						},
						"app_id": &graphql.ArgumentConfig{
							Description: "Filteres events by app id",
							Type:        graphql.String,
						},
						"count": &graphql.ArgumentConfig{
							Description: "Number of events to return",
							Type:        graphql.Int,
						},
						"before_id": &graphql.ArgumentConfig{
							Description: "Return only events before specified event id",
							Type:        graphql.Int,
						},
						"since_id": &graphql.ArgumentConfig{
							Description: "Return only events after specified event id",
							Type:        graphql.Int,
						},
					},
					Resolve: wrapResolveFunc(func(api *controllerAPI, p graphql.ResolveParams) (interface{}, error) {
						var appID string
						if i, ok := p.Args["app_id"]; ok {
							appID = i.(string)
						}
						var beforeID *int64
						if i, ok := p.Args["before_id"]; ok {
							if id, ok := i.(int); ok {
								id64 := int64(id)
								beforeID = &id64
							}
						}
						var sinceID *int64
						if i, ok := p.Args["since_id"]; ok {
							if id, ok := i.(int); ok {
								id64 := int64(id)
								sinceID = &id64
							}
						}
						var count int
						if i, ok := p.Args["count"]; ok {
							if n, ok := i.(int); ok {
								count = n
							}
						}
						var objectTypes []string
						if i, ok := p.Args["object_types"]; ok {
							for _, v := range i.([]interface{}) {
								objectTypes = append(objectTypes, v.(string))
							}
						}
						var objectID string
						if i, ok := p.Args["object_id"]; ok {
							objectID = i.(string)
						}
						return api.eventRepo.ListEvents(appID, objectTypes, objectID, beforeID, sinceID, count)
					}),
				},
			},
		}),
		Mutation: graphql.NewObject(graphql.ObjectConfig{
			Name: "RootMutation",
			Fields: graphql.Fields{
				"createArtifact": &graphql.Field{
					Type: artifactObject,
					Args: graphql.FieldConfigArgument{
						"id": &graphql.ArgumentConfig{
							Description: "UUID of artifact",
							Type:        graphql.String,
						},
						"type": &graphql.ArgumentConfig{
							Description: "Type of artifact",
							Type:        graphql.NewNonNull(graphql.String),
						},
						"uri": &graphql.ArgumentConfig{
							Description: "URI of artifact",
							Type:        graphql.NewNonNull(graphql.String),
						},
						"meta": &graphql.ArgumentConfig{
							Description: "Artifact metadata",
							Type:        metaObjectType,
						},
					},
					Resolve: wrapResolveFunc(func(api *controllerAPI, p graphql.ResolveParams) (interface{}, error) {
						stringValue := func(v interface{}) string {
							if v == nil {
								return ""
							}
							return v.(string)
						}
						metaValue := func(v interface{}) map[string]string {
							if v == nil {
								return nil
							}
							return v.(map[string]string)
						}
						artifact := &ct.Artifact{
							ID:   stringValue(p.Args["id"]),
							Type: host.ArtifactType(stringValue(p.Args["type"])),
							URI:  stringValue(p.Args["uri"]),
							Meta: metaValue(p.Args["meta"]),
						}
						return artifact, api.artifactRepo.Add(artifact)
					}),
				},
				"createRelease": &graphql.Field{
					Type: releaseObject,
					Args: graphql.FieldConfigArgument{
						"id": &graphql.ArgumentConfig{
							Description: "UUID of release",
							Type:        graphql.String,
						},
						"artifacts": &graphql.ArgumentConfig{
							Description: "UUIDs of artifacts to include in release",
							Type:        graphql.NewList(graphql.String),
						},
						"env": &graphql.ArgumentConfig{
							Description: "Env vars to include in release",
							Type:        envObjectType,
						},
						"meta": &graphql.ArgumentConfig{
							Description: "Metadata to include in release",
							Type:        metaObjectType,
						},
						"processes": &graphql.ArgumentConfig{
							Description: "Processes to include in release",
							Type:        processesObjectType,
						},
					},
					Resolve: wrapResolveFunc(func(api *controllerAPI, p graphql.ResolveParams) (interface{}, error) {
						release := &ct.Release{}
						if v, ok := p.Args["id"]; ok {
							release.ID = v.(string)
						}
						if v, ok := p.Args["artifacts"]; ok {
							list := v.([]interface{})
							release.ArtifactIDs = make([]string, len(list))
							for i, aid := range list {
								release.ArtifactIDs[i] = aid.(string)
							}
						}
						if v, ok := p.Args["env"]; ok {
							release.Env = v.(map[string]string)
						}
						if v, ok := p.Args["meta"]; ok {
							release.Meta = v.(map[string]string)
						}
						if v, ok := p.Args["processes"]; ok {
							b, err := json.Marshal(v)
							if err != nil {
								return nil, err
							}
							release.Processes = map[string]ct.ProcessType{}
							if err := json.Unmarshal(b, &release.Processes); err != nil {
								return nil, err
							}
						}
						return release, api.releaseRepo.Add(release)
					}),
				},
				"putFormation": &graphql.Field{
					Type: formationObject,
					Args: graphql.FieldConfigArgument{
						"app": &graphql.ArgumentConfig{
							Description: "UUID of app",
							Type:        graphql.NewNonNull(graphql.String),
						},
						"release": &graphql.ArgumentConfig{
							Description: "UUID of release",
							Type:        graphql.NewNonNull(graphql.String),
						},
						"processes": &graphql.ArgumentConfig{
							Description: "Count of each process to include in formation",
							Type:        processesObjectType,
						},
						"tags": &graphql.ArgumentConfig{
							Description: "Tags to include in formation",
							Type:        tagsObjectType,
						},
					},
					Resolve: wrapResolveFunc(func(api *controllerAPI, p graphql.ResolveParams) (interface{}, error) {
						formation := &ct.Formation{}
						if v, ok := p.Args["app"]; ok {
							formation.AppID = v.(string)
						}
						if v, ok := p.Args["release"]; ok {
							formation.ReleaseID = v.(string)
						}
						if v, ok := p.Args["formation"]; ok {
							formation.ReleaseID = v.(string)
						}
						if v, ok := p.Args["processes"]; ok {
							d, err := json.Marshal(v)
							if err != nil {
								return nil, err
							}
							formation.Processes = map[string]int{}
							if err := json.Unmarshal(d, &formation.Processes); err != nil {
								return nil, err
							}
						}
						if v, ok := p.Args["tags"]; ok {
							formation.Tags = v.(map[string]map[string]string)
						}
						return formation, api.formationRepo.Add(formation)
					}),
				},
			},
		}),
	})
	if err != nil {
		shutdown.Fatal(err)
	}
}

const (
	apiContextKey = "controllerAPI"
)

func contextWithAPI(api *controllerAPI, ctx context.Context) context.Context {
	ctx = context.WithValue(ctx, apiContextKey, api)
	return ctx
}

func (api *controllerAPI) GraphQLHandler() httphelper.HandlerFunc {
	h := graphqlhandler.New(&graphqlhandler.Config{
		Schema: &graphqlSchema,
		Pretty: false,
	})
	return func(ctx context.Context, w http.ResponseWriter, req *http.Request) {
		h.ContextHandler(contextWithAPI(api, ctx), w, req)
	}
}
