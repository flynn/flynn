//go:generate protoc -I/usr/local/include -I. -I${GOPATH}/src/github.com/grpc-ecosystem/grpc-gateway/third_party/googleapis --go_out=plugins=grpc:. ./controller.proto
package controllergrpc

import (
	"net/http"
	"path"
	"time"

	"github.com/flynn/flynn/controller/app"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/pkg/postgres"
	routerc "github.com/flynn/flynn/router/client"
	google_protobuf1 "github.com/golang/protobuf/ptypes/timestamp"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

type Config struct {
	DB                 *postgres.DB
	RouterClient       routerc.Client
	DefaultRouteDomain string

	appRepo *apprepo.Repo
}

type server struct {
	ControllerServer
	*Config
}

func (s *server) ListApps(context.Context, *ListAppsRequest) (*ListAppsResponse, error) {
	res, err := s.appRepo.List()
	if err != nil {
		return nil, err
	}
	ctApps := res.([]*ct.App)
	apps := make([]*App, len(ctApps))
	for i, a := range ctApps {
		apps[i] = &App{
			Name:          path.Join("apps", a.ID),
			DisplayName:   a.Name,
			Labels:        a.Meta,
			Strategy:      a.Strategy,
			Release:       path.Join("apps", a.ID, "releases", a.ReleaseID),
			DeployTimeout: a.DeployTimeout,
			CreateTime:    protobufTimestamp(a.CreatedAt),
			UpdateTime:    protobufTimestamp(a.UpdatedAt),
		}
	}
	return &ListAppsResponse{
		Apps: apps,
		// TODO(jvatic): Implement pagination (empty string = last page)
		NextPageToken: "",
	}, nil
}

func (s *server) GetApp(context.Context, *GetAppRequest) (*App, error) {
	return &App{}, nil
}

func (s *server) StreamAppLog(*StreamAppLogRequest, Controller_StreamAppLogServer) error {
	return nil
}

func (s *server) CreateRelease(context.Context, *CreateReleaseRequest) (*Release, error) {
	return &Release{}, nil
}

func (s *server) CreateDeployment(context.Context, *CreateDeploymentRequest) (*Deployment, error) {
	return &Deployment{}, nil
}

func (s *server) StreamEvents(*StreamEventsRequest, Controller_StreamEventsServer) error {
	return nil
}

func NewServer(c *Config) http.Handler {
	c.appRepo = apprepo.NewRepo(c.DB, c.DefaultRouteDomain, c.RouterClient)
	s := grpc.NewServer()
	RegisterControllerServer(s, &server{Config: c})
	reflection.Register(s)
	return s
}

func protobufTimestamp(ts *time.Time) *google_protobuf1.Timestamp {
	if ts == nil {
		return nil
	}
	return &google_protobuf1.Timestamp{
		Seconds: ts.Unix(),
		Nanos:   int32(ts.UnixNano()),
	}
}
