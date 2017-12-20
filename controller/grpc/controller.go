//go:generate protoc -I/usr/local/include -I. -I${GOPATH}/src/github.com/grpc-ecosystem/grpc-gateway/third_party/googleapis --go_out=Mgoogle/api/annotations.proto=github.com/grpc-ecosystem/grpc-gateway/third_party/googleapis/google/api,plugins=grpc:. ./controller.proto
package controllergrpc

import (
	"github.com/flynn/flynn/pkg/postgres"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

type Config struct {
	DB *postgres.DB
}

type server struct {
	*Config
}

func (s *server) ListApps(context.Context, *ListAppsRequest) (*ListAppsResponse, error) {
	return &ListAppsResponse{}, nil
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

func NewServer(c *Config) {
	s := grpc.NewServer()
	RegisterControllerServer(s, &server{c})
	reflection.Register(s)
	return s
}
