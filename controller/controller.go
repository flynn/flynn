package main

import (
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/que-go"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/jackc/pgx"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/julienschmidt/httprouter"
	"github.com/flynn/flynn/Godeps/_workspace/src/golang.org/x/net/context"
	"github.com/flynn/flynn/controller/name"
	"github.com/flynn/flynn/controller/schema"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/discoverd/client"
	logaggc "github.com/flynn/flynn/logaggregator/client"
	"github.com/flynn/flynn/pkg/cluster"
	"github.com/flynn/flynn/pkg/ctxhelper"
	"github.com/flynn/flynn/pkg/dialer"
	"github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/postgres"
	"github.com/flynn/flynn/pkg/shutdown"
	routerc "github.com/flynn/flynn/router/client"
	"github.com/flynn/flynn/router/types"
)

var ErrNotFound = errors.New("controller: resource not found")

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

	db := postgres.Wait("", "")

	if err := migrateDB(db.DB); err != nil {
		shutdown.Fatal(err)
	}

	pgxcfg, err := pgx.ParseURI(fmt.Sprintf("http://%s:%s@%s/%s", os.Getenv("PGUSER"), os.Getenv("PGPASSWORD"), db.Addr(), os.Getenv("PGDATABASE")))
	if err != nil {
		log.Fatal(err)
	}
	pgxcfg.Dial = dialer.Retry.Dial

	pgxpool, err := pgx.NewConnPool(pgx.ConnPoolConfig{
		ConnConfig:   pgxcfg,
		AfterConnect: que.PrepareStatements,
	})
	if err != nil {
		log.Fatal(err)
	}
	shutdown.BeforeExit(func() { pgxpool.Close() })

	cc, err := cluster.NewClient()
	if err != nil {
		shutdown.Fatal(err)
	}

	lc, err := logaggc.New("")
	if err != nil {
		shutdown.Fatal(err)
	}
	rc := routerc.New()

	hb, err := discoverd.DefaultClient.AddServiceAndRegisterInstance("flynn-controller", &discoverd.Instance{
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
		db:      db,
		cc:      cc,
		lc:      lc,
		rc:      rc,
		pgxpool: pgxpool,
		keys:    strings.Split(os.Getenv("AUTH_KEY"), ","),
	})
	shutdown.Fatal(http.ListenAndServe(addr, handler))
}

type handlerConfig struct {
	db      *postgres.DB
	cc      clusterClient
	lc      logaggc.Client
	rc      routerc.Client
	pgxpool *pgx.ConnPool
	keys    []string
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

	providerRepo := NewProviderRepo(c.db)
	keyRepo := NewKeyRepo(c.db)
	resourceRepo := NewResourceRepo(c.db)
	appRepo := NewAppRepo(c.db, os.Getenv("DEFAULT_ROUTE_DOMAIN"), c.rc)
	artifactRepo := NewArtifactRepo(c.db)
	releaseRepo := NewReleaseRepo(c.db)
	jobRepo := NewJobRepo(c.db)
	formationRepo := NewFormationRepo(c.db, appRepo, releaseRepo, artifactRepo)
	deploymentRepo := NewDeploymentRepo(c.db, c.pgxpool)

	api := controllerAPI{
		appRepo:        appRepo,
		releaseRepo:    releaseRepo,
		providerRepo:   providerRepo,
		formationRepo:  formationRepo,
		artifactRepo:   artifactRepo,
		jobRepo:        jobRepo,
		resourceRepo:   resourceRepo,
		deploymentRepo: deploymentRepo,
		clusterClient:  c.cc,
		logaggc:        c.lc,
		routerc:        c.rc,
	}

	httpRouter := httprouter.New()

	crud(httpRouter, "apps", ct.App{}, appRepo)
	crud(httpRouter, "releases", ct.Release{}, releaseRepo)
	crud(httpRouter, "providers", ct.Provider{}, providerRepo)
	crud(httpRouter, "artifacts", ct.Artifact{}, artifactRepo)
	crud(httpRouter, "keys", ct.Key{}, keyRepo)

	httpRouter.POST("/apps/:apps_id", httphelper.WrapHandler(api.UpdateApp))
	httpRouter.GET("/apps/:apps_id/log", httphelper.WrapHandler(api.appLookup(api.AppLog)))
	httpRouter.GET("/apps/:apps_id/events", httphelper.WrapHandler(api.appLookup(api.AppEvents)))

	httpRouter.PUT("/apps/:apps_id/formations/:releases_id", httphelper.WrapHandler(api.appLookup(api.PutFormation)))
	httpRouter.GET("/apps/:apps_id/formations/:releases_id", httphelper.WrapHandler(api.appLookup(api.GetFormation)))
	httpRouter.DELETE("/apps/:apps_id/formations/:releases_id", httphelper.WrapHandler(api.appLookup(api.DeleteFormation)))
	httpRouter.GET("/apps/:apps_id/formations", httphelper.WrapHandler(api.appLookup(api.ListFormations)))
	httpRouter.GET("/formations", httphelper.WrapHandler(api.GetFormations))

	httpRouter.POST("/apps/:apps_id/jobs", httphelper.WrapHandler(api.appLookup(api.RunJob)))
	httpRouter.GET("/apps/:apps_id/jobs/:jobs_id", httphelper.WrapHandler(api.appLookup(api.GetJob)))
	httpRouter.PUT("/apps/:apps_id/jobs/:jobs_id", httphelper.WrapHandler(api.appLookup(api.PutJob)))
	httpRouter.GET("/apps/:apps_id/jobs", httphelper.WrapHandler(api.appLookup(api.ListJobs)))
	httpRouter.DELETE("/apps/:apps_id/jobs/:jobs_id", httphelper.WrapHandler(api.appLookup(api.KillJob)))

	httpRouter.POST("/apps/:apps_id/deploy", httphelper.WrapHandler(api.appLookup(api.CreateDeployment)))
	httpRouter.GET("/deployments/:deployment_id", httphelper.WrapHandler(api.GetDeployment))

	httpRouter.PUT("/apps/:apps_id/release", httphelper.WrapHandler(api.appLookup(api.SetAppRelease)))
	httpRouter.GET("/apps/:apps_id/release", httphelper.WrapHandler(api.appLookup(api.GetAppRelease)))

	httpRouter.POST("/providers/:providers_id/resources", httphelper.WrapHandler(api.ProvisionResource))
	httpRouter.GET("/providers/:providers_id/resources", httphelper.WrapHandler(api.GetProviderResources))
	httpRouter.GET("/providers/:providers_id/resources/:resources_id", httphelper.WrapHandler(api.GetResource))
	httpRouter.PUT("/providers/:providers_id/resources/:resources_id", httphelper.WrapHandler(api.PutResource))
	httpRouter.GET("/apps/:apps_id/resources", httphelper.WrapHandler(api.appLookup(api.GetAppResources)))

	httpRouter.POST("/apps/:apps_id/routes", httphelper.WrapHandler(api.appLookup(api.CreateRoute)))
	httpRouter.GET("/apps/:apps_id/routes", httphelper.WrapHandler(api.appLookup(api.GetRouteList)))
	httpRouter.GET("/apps/:apps_id/routes/:routes_type/:routes_id", httphelper.WrapHandler(api.appLookup(api.GetRoute)))
	httpRouter.DELETE("/apps/:apps_id/routes/:routes_type/:routes_id", httphelper.WrapHandler(api.appLookup(api.DeleteRoute)))

	return httphelper.ContextInjector("controller",
		httphelper.NewRequestLogger(muxHandler(httpRouter, c.keys)))
}

func muxHandler(main http.Handler, authKeys []string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		httphelper.CORSAllowAllHandler(w, r)
		if r.URL.Path == "/ping" || r.Method == "OPTIONS" {
			w.WriteHeader(200)
			return
		}
		_, password, _ := parseBasicAuth(r.Header)
		if password == "" && strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
			password = r.URL.Query().Get("key")
		}
		var authed bool
		for _, k := range authKeys {
			if len(password) == len(k) && subtle.ConstantTimeCompare([]byte(password), []byte(k)) == 1 {
				authed = true
				break
			}
		}
		if !authed {
			w.WriteHeader(401)
			return
		}
		main.ServeHTTP(w, r)
	})
}

type controllerAPI struct {
	appRepo        *AppRepo
	releaseRepo    *ReleaseRepo
	providerRepo   *ProviderRepo
	formationRepo  *FormationRepo
	artifactRepo   *ArtifactRepo
	jobRepo        *JobRepo
	resourceRepo   *ResourceRepo
	deploymentRepo *DeploymentRepo
	clusterClient  clusterClient
	logaggc        logaggc.Client
	routerc        routerc.Client
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
	return "controller/apps/" + appID
}

func (c *controllerAPI) getRoute(ctx context.Context) (*router.Route, error) {
	params, _ := ctxhelper.ParamsFromContext(ctx)
	route, err := c.routerc.GetRoute(params.ByName("routes_type"), params.ByName("routes_id"))
	if err == routerc.ErrNotFound || err == nil && route.ParentRef != routeParentRef(c.getApp(ctx).ID) {
		err = ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return route, err
}

func parseBasicAuth(h http.Header) (username, password string, err error) {
	s := strings.SplitN(h.Get("Authorization"), " ", 2)

	if len(s) != 2 {
		return "", "", errors.New("failed to parse authentication string ")
	}
	if s[0] != "Basic" {
		return "", "", fmt.Errorf("authorization scheme is %v, not Basic ", s[0])
	}

	c, err := base64.StdEncoding.DecodeString(s[1])
	if err != nil {
		return "", "", errors.New("failed to parse base64 basic credentials")
	}

	s = strings.SplitN(string(c), ":", 2)
	if len(s) != 2 {
		return "", "", errors.New("failed to parse basic credentials")
	}

	return s[0], s[1], nil
}
