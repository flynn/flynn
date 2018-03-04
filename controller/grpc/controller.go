//go:generate protoc -I/usr/local/include -I. -I${GOPATH}/src/github.com/grpc-ecosystem/grpc-gateway/third_party/googleapis --go_out=plugins=grpc:. ./controller.proto
package controllergrpc

import (
	"os"
	"path"
	"strings"
	"time"

	"github.com/flynn/flynn/controller/apps"
	"github.com/flynn/flynn/controller/artifacts"
	"github.com/flynn/flynn/controller/releases"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/host/resource"
	host "github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/pkg/postgres"
	routerc "github.com/flynn/flynn/router/client"
	que "github.com/flynn/que-go"
	"github.com/golang/protobuf/ptypes"
	durpb "github.com/golang/protobuf/ptypes/duration"
	tspb "github.com/golang/protobuf/ptypes/timestamp"
	"github.com/opencontainers/runc/libcontainer/configs"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

type Config struct {
	DB                 *postgres.DB
	RouterClient       routerc.Client
	DefaultRouteDomain string

	appRepo     *apps.Repo
	releaseRepo *releases.Repo
}

func NewServer(c *Config) *grpc.Server {
	c.appRepo = apps.NewRepo(c.DB, c.DefaultRouteDomain, c.RouterClient)
	q := que.NewClient(c.DB.ConnPool)
	artifactRepo := artifacts.NewRepo(c.DB)
	c.releaseRepo = releases.NewRepo(c.DB, artifactRepo, q)
	s := grpc.NewServer()
	RegisterControllerServer(s, &server{Config: c})
	// Register reflection service on gRPC server.
	reflection.Register(s)
	return s
}

type server struct {
	ControllerServer
	*Config
}

func parseAppID(name string) string {
	parts := strings.Split(name, "/")
	if len(parts) == 2 {
		return parts[1]
	}
	return ""
}

func convertApp(a *ct.App) *App {
	return &App{
		Name:          path.Join("apps", a.ID),
		DisplayName:   a.Name,
		Labels:        a.Meta,
		Strategy:      a.Strategy,
		Release:       path.Join("apps", a.ID, "releases", a.ReleaseID),
		DeployTimeout: a.DeployTimeout,
		CreateTime:    timestampProto(a.CreatedAt),
		UpdateTime:    timestampProto(a.UpdatedAt),
	}
}

func (s *server) ListApps(ctx context.Context, req *ListAppsRequest) (*ListAppsResponse, error) {
	res, err := s.appRepo.List()
	if err != nil {
		return nil, err
	}
	ctApps := res.([]*ct.App)
	apps := make([]*App, len(ctApps))
	for i, a := range ctApps {
		apps[i] = convertApp(a)
	}
	return &ListAppsResponse{
		Apps: apps,
		// TODO(jvatic): Implement pagination (empty string = last page)
		NextPageToken: "",
	}, nil
}

func (s *server) GetApp(ctx context.Context, req *GetAppRequest) (*App, error) {
	res, err := s.appRepo.Get(parseAppID(req.Name))
	if err != nil {
		return nil, err
	}
	return convertApp(res.(*ct.App)), nil
}

func (s *server) StreamAppLog(*StreamAppLogRequest, Controller_StreamAppLogServer) error {
	return nil
}

func (s *server) CreateRelease(ctx context.Context, req *CreateReleaseRequest) (*Release, error) {
	r := req.Release
	processes := make(map[string]ct.ProcessType, len(r.Processes))
	for k, v := range r.Processes {
		ports := make([]ct.Port, len(v.Ports))
		for i, p := range v.Ports {
			var service *host.Service
			if p.Service != nil {
				var healthCheck *host.HealthCheck
				if p.Service.Check != nil {
					healthCheck = &host.HealthCheck{
						Type:         p.Service.Check.Type,
						Interval:     parseProtoDuration(p.Service.Check.Interval),
						Threshold:    int(p.Service.Check.Threshold),
						KillDown:     p.Service.Check.KillDown,
						StartTimeout: parseProtoDuration(p.Service.Check.StartTimeout),
						Path:         p.Service.Check.Path,
						Host:         p.Service.Check.Host,
						Match:        p.Service.Check.Match,
						Status:       int(p.Service.Check.Status),
					}
				}
				service = &host.Service{
					Name:   p.Service.DisplayName,
					Create: p.Service.Create,
					Check:  healthCheck,
				}
			}
			ports[i] = ct.Port{
				Port:    int(p.Port),
				Proto:   p.Proto,
				Service: service,
			}
		}
		volumes := make([]ct.VolumeReq, len(v.Volumes))
		for i, v := range v.Volumes {
			volumes[i] = ct.VolumeReq{
				Path:         v.Path,
				DeleteOnStop: v.DeleteOnStop,
			}
		}
		resources := make(resource.Resources, len(v.Resources))
		for k, v := range v.Resources {
			var r *int64
			if v.Request > 0 {
				r = &v.Request
			}
			var l *int64
			if v.Limit > 0 {
				l = &v.Limit
			}
			// TODO(jvatic): Should the proto spec be changed to distinguish between zero and nil?
			resources[resource.Type(k)] = resource.Spec{
				Request: r,
				Limit:   l,
			}
		}
		mounts := make([]host.Mount, len(v.Mounts))
		for i, v := range v.Mounts {
			mounts[i] = host.Mount{
				Location:  v.Location,
				Target:    v.Target,
				Writeable: v.Writable,
				Device:    v.Device,
				Data:      v.Data,
				Flags:     int(v.Flags),
			}
		}
		allowedDevices := make([]*configs.Device, len(v.AllowedDevices))
		for i, v := range v.AllowedDevices {
			allowedDevices[i] = &configs.Device{
				Type:        rune(v.Type),
				Path:        v.Path,
				Major:       v.Major,
				Minor:       v.Minor,
				Permissions: v.Permissions,
				FileMode:    os.FileMode(v.FileMode),
				Uid:         v.Uid,
				Gid:         v.Gid,
				Allow:       v.Allow,
			}
		}
		processes[k] = ct.ProcessType{
			Args:              v.Args,
			Env:               v.Env,
			Ports:             ports,
			Volumes:           volumes,
			Omni:              v.Omni,
			HostNetwork:       v.HostNetwork,
			HostPIDNamespace:  v.HostPidNamespace,
			Service:           v.Service,
			Resurrect:         v.Resurrect,
			Resources:         resources,
			Mounts:            mounts,
			LinuxCapabilities: v.LinuxCapabilities,
			AllowedDevices:    allowedDevices,
			WriteableCgroups:  v.WriteableCgroups,
		}
	}
	ctRelease := &ct.Release{
		AppID:       parseAppID(req.Parent),
		ArtifactIDs: r.Artifacts,
		Env:         r.Env,
		Meta:        r.Labels,
		Processes:   processes,
	}
	if err := s.releaseRepo.Add(ctRelease); err != nil {
		return nil, err
	}
	return &Release{
		Name:       path.Join("apps", ctRelease.AppID, "releases", ctRelease.ID),
		Artifacts:  ctRelease.ArtifactIDs,
		Env:        ctRelease.Env,
		Labels:     ctRelease.Meta,
		Processes:  r.Processes, // Assumes repeaseRepo.Add didn't modify the input
		CreateTime: timestampProto(ctRelease.CreatedAt),
	}, nil
}

func (s *server) CreateDeployment(context.Context, *CreateDeploymentRequest) (*Deployment, error) {
	return &Deployment{}, nil
}

func (s *server) StreamEvents(*StreamEventsRequest, Controller_StreamEventsServer) error {
	return nil
}

func parseProtoDuration(dur *durpb.Duration) time.Duration {
	d, _ := ptypes.Duration(dur)
	return d
}

func timestampProto(t *time.Time) *tspb.Timestamp {
	if t == nil {
		return nil
	}
	tp, _ := ptypes.TimestampProto(*t)
	return tp
}
