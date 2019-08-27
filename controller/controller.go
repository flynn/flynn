package main

import (
	"encoding/hex"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/flynn/flynn/controller/data"
	"github.com/flynn/flynn/controller/name"
	"github.com/flynn/flynn/controller/schema"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/controller/utils"
	discoverd "github.com/flynn/flynn/discoverd/client"
	logaggc "github.com/flynn/flynn/logaggregator/client"
	logagg "github.com/flynn/flynn/logaggregator/types"
	"github.com/flynn/flynn/pkg/cluster"
	"github.com/flynn/flynn/pkg/ctxhelper"
	"github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/postgres"
	"github.com/flynn/flynn/pkg/shutdown"
	"github.com/flynn/flynn/pkg/status"
	router "github.com/flynn/flynn/router/types"
	"github.com/flynn/que-go"
	"github.com/inconshreveable/log15"
	"github.com/julienschmidt/httprouter"
	"golang.org/x/net/context"
)

var logger = log15.New("component", "controller")

var ErrNotFound = ct.ErrNotFound
var ErrShutdown = errors.New("controller: shutting down")

var schemaRoot = "/etc/flynn-controller/jsonschema"

func main() {
	defer shutdown.Exit()

	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}
	addr := ":" + port

	if seed := os.Getenv("NAME_SEED"); seed != "" {
		s, err := hex.DecodeString(seed)
		if err != nil {
			log.Fatalln("error decoding NAME_SEED:", err)
		}
		name.SetSeed(s)
	}

	db := data.OpenAndMigrateDB(nil)
	shutdown.BeforeExit(func() { db.Close() })

	lc, err := logaggc.New("")
	if err != nil {
		shutdown.Fatal(err)
	}

	doneCh := make(chan struct{})
	shutdown.BeforeExit(func() { close(doneCh) })

	// Listen for database migration, reset connpool on new migration
	go postgres.ResetOnMigration(db, logger, doneCh)

	hb, err := discoverd.DefaultClient.AddServiceAndRegisterInstance("controller", &discoverd.Instance{
		Addr:  addr,
		Proto: "http",
		Meta: map[string]string{
			"AUTH_KEY": os.Getenv("AUTH_KEY"),
		},
	})
	if err != nil {
		shutdown.Fatal(err)
	}

	shutdown.BeforeExit(func() {
		hb.Close()
	})

	handler := appHandler(handlerConfig{
		db:     db,
		cc:     utils.ClusterClientWrapper(cluster.NewClient()),
		lc:     lc,
		keys:   strings.Split(os.Getenv("AUTH_KEY"), ","),
		keyIDs: strings.Split(os.Getenv("AUTH_KEY_IDS"), ","),
		caCert: []byte(os.Getenv("CA_CERT")),
	})
	shutdown.Fatal(http.ListenAndServe(addr, handler))
}

type logClient interface {
	GetLog(channelID string, options *logagg.LogOpts) (io.ReadCloser, error)
}

type handlerConfig struct {
	db     *postgres.DB
	cc     utils.ClusterClient
	lc     logClient
	keys   []string
	keyIDs []string
	caCert []byte
}

// NOTE: this is temporary until httphelper supports custom errors
func respondWithError(w http.ResponseWriter, err error) {
	switch v := err.(type) {
	case ct.ValidationError:
		httphelper.ValidationError(w, v.Field, v.Message)
	default:
		if err == ErrNotFound {
			w.WriteHeader(404)
			return
		}
		httphelper.Error(w, err)
	}
}

func appHandler(c handlerConfig) http.Handler {
	err := schema.Load(schemaRoot)
	if err != nil {
		shutdown.Fatal(err)
	}

	q := que.NewClient(c.db.ConnPool)
	domainMigrationRepo := data.NewDomainMigrationRepo(c.db)
	providerRepo := data.NewProviderRepo(c.db)
	resourceRepo := data.NewResourceRepo(c.db)
	routeRepo := data.NewRouteRepo(c.db)
	appRepo := data.NewAppRepo(c.db, os.Getenv("DEFAULT_ROUTE_DOMAIN"), routeRepo)
	artifactRepo := data.NewArtifactRepo(c.db)
	releaseRepo := data.NewReleaseRepo(c.db, artifactRepo, q)
	jobRepo := data.NewJobRepo(c.db)
	formationRepo := data.NewFormationRepo(c.db, appRepo, releaseRepo, artifactRepo)
	deploymentRepo := data.NewDeploymentRepo(c.db, appRepo, releaseRepo, formationRepo)
	eventRepo := data.NewEventRepo(c.db)
	backupRepo := data.NewBackupRepo(c.db)
	sinkRepo := data.NewSinkRepo(c.db)
	volumeRepo := data.NewVolumeRepo(c.db)

	api := controllerAPI{
		domainMigrationRepo: domainMigrationRepo,
		appRepo:             appRepo,
		releaseRepo:         releaseRepo,
		providerRepo:        providerRepo,
		formationRepo:       formationRepo,
		artifactRepo:        artifactRepo,
		jobRepo:             jobRepo,
		routeRepo:           routeRepo,
		resourceRepo:        resourceRepo,
		deploymentRepo:      deploymentRepo,
		eventRepo:           eventRepo,
		backupRepo:          backupRepo,
		sinkRepo:            sinkRepo,
		volumeRepo:          volumeRepo,
		clusterClient:       c.cc,
		logaggc:             c.lc,
		que:                 q,
		caCert:              c.caCert,
		config:              c,
	}

	shutdown.BeforeExit(api.Shutdown)

	httpRouter := httprouter.New()

	crud(httpRouter, "apps", ct.App{}, appRepo)
	crud(httpRouter, "releases", ct.Release{}, releaseRepo)
	crud(httpRouter, "providers", ct.Provider{}, providerRepo)
	crud(httpRouter, "artifacts", ct.Artifact{}, artifactRepo)

	httpRouter.Handler("GET", status.Path, status.Handler(func() status.Status {
		if err := c.db.Exec("ping"); err != nil {
			return status.Unhealthy
		}
		return status.Healthy
	}))

	httpRouter.GET("/ca-cert", httphelper.WrapHandler(api.GetCACert))

	httpRouter.GET("/backup", httphelper.WrapHandler(api.GetBackup))

	httpRouter.PUT("/domain", httphelper.WrapHandler(api.MigrateDomain))

	httpRouter.POST("/apps/:apps_id", httphelper.WrapHandler(api.UpdateApp))
	httpRouter.GET("/apps/:apps_id/log", httphelper.WrapHandler(api.appLookup(api.AppLog)))
	httpRouter.DELETE("/apps/:apps_id", httphelper.WrapHandler(api.appLookup(api.DeleteApp)))
	httpRouter.DELETE("/apps/:apps_id/releases/:releases_id", httphelper.WrapHandler(api.appLookup(api.DeleteRelease)))
	httpRouter.POST("/apps/:apps_id/gc", httphelper.WrapHandler(api.appLookup(api.ScheduleAppGarbageCollection)))

	httpRouter.PUT("/apps/:apps_id/formations/:releases_id", httphelper.WrapHandler(api.appLookup(api.PutFormation)))
	httpRouter.GET("/apps/:apps_id/formations/:releases_id", httphelper.WrapHandler(api.appLookup(api.GetFormation)))
	httpRouter.DELETE("/apps/:apps_id/formations/:releases_id", httphelper.WrapHandler(api.appLookup(api.DeleteFormation)))
	httpRouter.GET("/apps/:apps_id/formations", httphelper.WrapHandler(api.appLookup(api.ListFormations)))
	httpRouter.GET("/formations", httphelper.WrapHandler(api.GetFormations))

	httpRouter.PUT("/apps/:apps_id/scale/:releases_id", httphelper.WrapHandler(api.appLookup(api.PutScaleRequest)))

	httpRouter.POST("/apps/:apps_id/jobs", httphelper.WrapHandler(api.appLookup(api.RunJob)))
	httpRouter.GET("/apps/:apps_id/jobs/:jobs_id", httphelper.WrapHandler(api.GetJob))
	httpRouter.PUT("/apps/:apps_id/jobs/:jobs_id", httphelper.WrapHandler(api.PutJob))
	httpRouter.GET("/apps/:apps_id/jobs", httphelper.WrapHandler(api.appLookup(api.ListJobs)))
	httpRouter.DELETE("/apps/:apps_id/jobs/:jobs_id", httphelper.WrapHandler(api.KillJob))
	httpRouter.GET("/active-jobs", httphelper.WrapHandler(api.ListActiveJobs))

	httpRouter.POST("/apps/:apps_id/deploy", httphelper.WrapHandler(api.appLookup(api.CreateDeployment)))
	httpRouter.GET("/apps/:apps_id/deployments", httphelper.WrapHandler(api.appLookup(api.ListDeployments)))
	httpRouter.GET("/deployments/:deployment_id", httphelper.WrapHandler(api.GetDeployment))

	httpRouter.PUT("/apps/:apps_id/release", httphelper.WrapHandler(api.appLookup(api.SetAppRelease)))
	httpRouter.GET("/apps/:apps_id/release", httphelper.WrapHandler(api.appLookup(api.GetAppRelease)))
	httpRouter.GET("/apps/:apps_id/releases", httphelper.WrapHandler(api.appLookup(api.GetAppReleases)))

	httpRouter.GET("/resources", httphelper.WrapHandler(api.GetResources))
	httpRouter.POST("/providers/:providers_id/resources", httphelper.WrapHandler(api.ProvisionResource))
	httpRouter.GET("/providers/:providers_id/resources", httphelper.WrapHandler(api.GetProviderResources))
	httpRouter.GET("/providers/:providers_id/resources/:resources_id", httphelper.WrapHandler(api.GetResource))
	httpRouter.PUT("/providers/:providers_id/resources/:resources_id", httphelper.WrapHandler(api.PutResource))
	httpRouter.DELETE("/providers/:providers_id/resources/:resources_id", httphelper.WrapHandler(api.DeleteResource))
	httpRouter.PUT("/providers/:providers_id/resources/:resources_id/apps/:app_id", httphelper.WrapHandler(api.AddResourceApp))
	httpRouter.DELETE("/providers/:providers_id/resources/:resources_id/apps/:app_id", httphelper.WrapHandler(api.DeleteResourceApp))
	httpRouter.GET("/apps/:apps_id/resources", httphelper.WrapHandler(api.appLookup(api.GetAppResources)))

	httpRouter.GET("/routes", httphelper.WrapHandler(api.GetRouteList))
	httpRouter.POST("/apps/:apps_id/routes", httphelper.WrapHandler(api.appLookup(api.CreateRoute)))
	httpRouter.GET("/apps/:apps_id/routes", httphelper.WrapHandler(api.appLookup(api.GetAppRouteList)))
	httpRouter.GET("/apps/:apps_id/routes/:routes_type/:routes_id", httphelper.WrapHandler(api.appLookup(api.GetRoute)))
	httpRouter.PUT("/apps/:apps_id/routes/:routes_type/:routes_id", httphelper.WrapHandler(api.appLookup(api.UpdateRoute)))
	httpRouter.DELETE("/apps/:apps_id/routes/:routes_type/:routes_id", httphelper.WrapHandler(api.appLookup(api.DeleteRoute)))

	httpRouter.POST("/apps/:apps_id/meta", httphelper.WrapHandler(api.appLookup(api.UpdateApp)))

	httpRouter.GET("/events", httphelper.WrapHandler(api.Events))
	httpRouter.GET("/events/:id", httphelper.WrapHandler(api.GetEvent))

	httpRouter.GET("/volumes", httphelper.WrapHandler(api.GetVolumes))
	httpRouter.PUT("/volumes/:volume_id", httphelper.WrapHandler(api.PutVolume))
	httpRouter.GET("/apps/:apps_id/volumes", httphelper.WrapHandler(api.appLookup(api.GetAppVolumes)))
	httpRouter.GET("/apps/:apps_id/volumes/:volume_id", httphelper.WrapHandler(api.appLookup(api.GetVolume)))
	httpRouter.PUT("/apps/:apps_id/volumes/:volume_id/decommission", httphelper.WrapHandler(api.appLookup(api.DecommissionVolume)))

	httpRouter.POST("/sinks", httphelper.WrapHandler(api.CreateSink))
	httpRouter.GET("/sinks", httphelper.WrapHandler(api.GetSinks))
	httpRouter.GET("/sinks/:sink_id", httphelper.WrapHandler(api.GetSink))
	httpRouter.DELETE("/sinks/:sink_id", httphelper.WrapHandler(api.DeleteSink))

	if os.Getenv("AUDIT_LOG") == "true" {
		return httphelper.ContextInjector("controller",
			httphelper.NewRequestLoggerCustom(muxHandler(httpRouter, c.keyIDs, c.keys), auditLoggerFn))
	}
	return httphelper.ContextInjector("controller",
		httphelper.NewRequestLogger(muxHandler(httpRouter, c.keyIDs, c.keys)))
}

func muxHandler(main http.Handler, authIDs, authKeys []string) http.Handler {
	authorizer := utils.NewAuthorizer(authKeys, authIDs)
	return httphelper.CORSAllowAll.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if shutdown.IsActive() {
			httphelper.ServiceUnavailableError(w, ErrShutdown.Error())
			return
		}

		if r.URL.Path == "/ping" {
			w.WriteHeader(200)
			return
		}
		_, password, _ := r.BasicAuth()
		if password == "" && r.URL.Path == "/ca-cert" {
			main.ServeHTTP(w, r)
			return
		}
		if password == "" && (strings.Contains(r.Header.Get("Accept"), "text/event-stream") || r.URL.Path == "/backup") {
			password = r.URL.Query().Get("key")
		}
		auth, err := authorizer.Authorize(password)
		if err != nil {
			w.WriteHeader(401)
			return
		}
		if auth.ID != "" {
			r.Header.Set("Flynn-Auth-Key-ID", auth.ID)
		}
		main.ServeHTTP(w, r)
	}))
}

type controllerAPI struct {
	domainMigrationRepo *data.DomainMigrationRepo
	appRepo             *data.AppRepo
	releaseRepo         *data.ReleaseRepo
	providerRepo        *data.ProviderRepo
	formationRepo       *data.FormationRepo
	artifactRepo        *data.ArtifactRepo
	jobRepo             *data.JobRepo
	routeRepo           *data.RouteRepo
	resourceRepo        *data.ResourceRepo
	deploymentRepo      *data.DeploymentRepo
	eventRepo           *data.EventRepo
	backupRepo          *data.BackupRepo
	sinkRepo            *data.SinkRepo
	volumeRepo          *data.VolumeRepo
	clusterClient       utils.ClusterClient
	logaggc             logClient
	que                 *que.Client
	caCert              []byte
	config              handlerConfig

	eventListener    *data.EventListener
	eventListenerMtx sync.Mutex
}

func (c *controllerAPI) getApp(ctx context.Context) *ct.App {
	return ctx.Value("app").(*ct.App)
}

func (c *controllerAPI) getRelease(ctx context.Context) (*ct.Release, error) {
	params, _ := ctxhelper.ParamsFromContext(ctx)
	data, err := c.releaseRepo.Get(params.ByName("releases_id"))
	if err != nil {
		return nil, err
	}
	return data.(*ct.Release), nil
}

func (c *controllerAPI) getProvider(ctx context.Context) (*ct.Provider, error) {
	params, _ := ctxhelper.ParamsFromContext(ctx)
	data, err := c.providerRepo.Get(params.ByName("providers_id"))
	if err != nil {
		return nil, err
	}
	return data.(*ct.Provider), nil
}

func (c *controllerAPI) appLookup(handler httphelper.HandlerFunc) httphelper.HandlerFunc {
	return func(ctx context.Context, w http.ResponseWriter, req *http.Request) {
		params, _ := ctxhelper.ParamsFromContext(ctx)
		data, err := c.appRepo.Get(params.ByName("apps_id"))
		if err != nil {
			respondWithError(w, err)
			return
		}
		ctx = context.WithValue(ctx, "app", data.(*ct.App))
		handler(ctx, w, req)
	}
}

func routeParentRef(appID string) string {
	return ct.RouteParentRefPrefix + appID
}

func (c *controllerAPI) getRoute(ctx context.Context) (*router.Route, error) {
	params, _ := ctxhelper.ParamsFromContext(ctx)
	route, err := c.routeRepo.Get(params.ByName("routes_type"), params.ByName("routes_id"))
	if err == data.ErrRouteNotFound || err == nil && route.ParentRef != routeParentRef(c.getApp(ctx).ID) {
		err = ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return route, err
}

func (c *controllerAPI) GetCACert(_ context.Context, w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/x-x509-ca-cert")
	w.Write(c.caCert)
}

func (c *controllerAPI) Shutdown() {
	if c.eventListener != nil {
		c.eventListener.CloseWithError(ErrShutdown)
	}
}
