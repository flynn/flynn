package main

import (
	"context"
	"net"
	"os"

	pb "github.com/flynn/flynn/controller/grpc"
	"github.com/flynn/flynn/pkg/shutdown"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

type server struct {
	appRepo *AppRepo
}

func (s *server) GetApp(ctx context.Context, in *pb.AppRequest) (*pb.App, error) {
	app, err := s.appRepo.selectApp(in.Id)
	if err != nil {
		return nil, err
	}
	return pb.NewApp(app), nil
}

func serveGRPC(l *net.Listener, c handlerConfig) {
	appRepo := NewAppRepo(c.db, os.Getenv("DEFAULT_ROUTE_DOMAIN"), c.rc)
	s := grpc.NewServer()
	pb.RegisterControllerServer(s, &server{
		appRepo: appRepo,
	})
	// Register reflection service on gRPC server.
	reflection.Register(s)
	if err := s.Serve(l); err != nil {
		shutdown.Fatal(err)
	}
}
