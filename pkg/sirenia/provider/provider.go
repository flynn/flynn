package provider

import (
	"fmt"
	"sync"

	"github.com/flynn/flynn/pkg/provider"
	sirenia "github.com/flynn/flynn/pkg/sirenia/client"
	"github.com/flynn/flynn/pkg/sirenia/scale"
	"github.com/flynn/flynn/pkg/sirenia/state"
	"github.com/flynn/flynn/pkg/status/protobuf"
	"golang.org/x/net/context"
	"gopkg.in/inconshreveable/log15.v2"
)

type Database interface {
	Ping() error
	Provision() (string, map[string]string, error)
	Deprovision(string) error
	Logger() log15.Logger
}

type Provider struct {
	sync.Mutex

	db            Database
	appID         string
	controllerKey string
	serviceHost   string
	serviceAddr   string
	serviceName   string
	singleton     bool
	enableScaling bool

	scaledUp bool
}

func NewProvider(db Database, appID, controllerKey, serviceHost, serviceAddr, serviceName string, singleton, enableScaling bool) *Provider {
	return &Provider{
		db:            db,
		appID:         appID,
		controllerKey: controllerKey,
		serviceHost:   serviceHost,
		serviceAddr:   serviceAddr,
		serviceName:   serviceName,
		singleton:     singleton,
		enableScaling: enableScaling,
	}
}

func (p *Provider) Status(ctx context.Context, _ *status.StatusRequest) (*status.StatusReply, error) {
	logger := p.db.Logger().New("fn", "Status")

	logger.Info("checking status", "host", p.serviceHost)
	if ss, err := sirenia.NewClient(p.serviceAddr).Status(); err == nil && ss.Database != nil && ss.Database.ReadWrite {
		logger.Info("database is up, skipping scale check")
	} else if p.enableScaling { // check scale if scaling is enabled
		scaled, err := scale.CheckScale(p.appID, p.controllerKey, p.serviceName, p.db.Logger())
		if err != nil {
			return &status.StatusReply{
				Status: status.StatusReply_UNHEALTHY,
			}, err
		}

		// Cluster has yet to be scaled, return healthy
		if !scaled {
			return &status.StatusReply{}, nil
		}
	}

	if err := p.db.Ping(); err != nil {
		return &status.StatusReply{
			Status: status.StatusReply_UNHEALTHY,
		}, err
	}
	return &status.StatusReply{}, nil
}

func (p *Provider) Provision(ctx context.Context, _ *provider.ProvisionRequest) (*provider.ProvisionReply, error) {
	if err := p.scaleUp(); err != nil {
		return nil, err
	}

	id, env, err := p.db.Provision()
	if err != nil {
		return nil, err
	}

	return &provider.ProvisionReply{Id: id, Env: env}, nil
}

func (p *Provider) Deprovision(ctx context.Context, req *provider.DeprovisionRequest) (*provider.DeprovisionReply, error) {
	if err := p.db.Deprovision(req.Id); err != nil {
		return nil, err
	}
	return &provider.DeprovisionReply{}, nil
}

func (p *Provider) GetTunables(ctx context.Context, req *provider.GetTunablesRequest) (*provider.GetTunablesReply, error) {
	reply := &provider.GetTunablesReply{}
	sc := sirenia.NewClient(p.serviceAddr)
	tunables, err := sc.GetTunables()
	if err != nil {
		return reply, err
	}
	reply.Tunables = tunables.Data
	reply.Version = tunables.Version
	return reply, nil
}

func (p *Provider) UpdateTunables(ctx context.Context, req *provider.UpdateTunablesRequest) (*provider.UpdateTunablesReply, error) {
	reply := &provider.UpdateTunablesReply{}
	if p.singleton {
		return reply, fmt.Errorf("Tunables can't be updated on singleton clusters")
	}
	update := &state.Tunables{
		Data:    req.Tunables,
		Version: req.Version,
	}
	sc := sirenia.NewClient(p.serviceAddr)
	err := sc.UpdateTunables(update)
	return reply, err
}

func (p *Provider) scaleUp() error {
	p.Lock()
	defer p.Unlock()

	if !p.enableScaling || p.scaledUp {
		return nil
	}

	err := scale.ScaleUp(p.appID, p.controllerKey, p.serviceAddr, p.serviceName, p.singleton, p.db.Logger())
	if err != nil {
		return err
	}

	p.scaledUp = true
	return nil
}
