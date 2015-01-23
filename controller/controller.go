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

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/julienschmidt/httprouter"
	"github.com/flynn/flynn/controller/name"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/cluster"
	"github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/postgres"
	"github.com/flynn/flynn/pkg/resource"
	"github.com/flynn/flynn/pkg/shutdown"
	routerc "github.com/flynn/flynn/router/client"
	"github.com/flynn/flynn/router/types"
)

var ErrNotFound = errors.New("controller: resource not found")

func main() {
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

	postgres.Wait("")
	db, err := postgres.Open("", "")
	if err != nil {
		log.Fatal(err)
	}

	if err := migrateDB(db.DB); err != nil {
		log.Fatal(err)
	}

	cc, err := cluster.NewClient()
	if err != nil {
		log.Fatal(err)
	}

	sc, err := routerc.New()
	if err != nil {
		log.Fatal(err)
	}

	if err := discoverd.Register("flynn-controller", addr); err != nil {
		log.Fatal(err)
	}

	shutdown.BeforeExit(func() {
		discoverd.Unregister("flynn-controller", addr)
	})

	handler := appHandler(handlerConfig{db: db, cc: cc, sc: sc, dc: discoverd.DefaultClient, key: os.Getenv("AUTH_KEY")})
	log.Fatal(http.ListenAndServe(addr, handler))
}

type handlerConfig struct {
	db  *postgres.DB
	cc  clusterClient
	sc  routerc.Client
	dc  resource.DiscoverdClient
	key string
}

// NOTE: this is temporary until httphelper supports custom errors
func respondWithError(w http.ResponseWriter, err error) {
	switch err.(type) {
	case ct.ValidationError:
		httphelper.JSON(w, 400, err)
	default:
		if err == ErrNotFound {
			w.WriteHeader(404)
			return
		}
		httphelper.Error(w, err)
	}
}

func appHandler(c handlerConfig) http.Handler {
	providerRepo := NewProviderRepo(c.db)
	keyRepo := NewKeyRepo(c.db)
	resourceRepo := NewResourceRepo(c.db)
	appRepo := NewAppRepo(c.db, os.Getenv("DEFAULT_ROUTE_DOMAIN"), c.sc)
	artifactRepo := NewArtifactRepo(c.db)
	releaseRepo := NewReleaseRepo(c.db)
	jobRepo := NewJobRepo(c.db)
	formationRepo := NewFormationRepo(c.db, appRepo, releaseRepo, artifactRepo)

	api := controllerAPI{
		appRepo:         appRepo,
		releaseRepo:     releaseRepo,
		providerRepo:    providerRepo,
		formationRepo:   formationRepo,
		artifactRepo:    artifactRepo,
		jobRepo:         jobRepo,
		resourceRepo:    resourceRepo,
		clusterClient:   c.cc,
		discoverdClient: c.dc,
		routerc:         c.sc,
	}

	httpRouter := httprouter.New()

	crud(httpRouter, "apps", ct.App{}, appRepo)
	crud(httpRouter, "releases", ct.Release{}, releaseRepo)
	crud(httpRouter, "providers", ct.Provider{}, providerRepo)
	crud(httpRouter, "artifacts", ct.Artifact{}, artifactRepo)
	crud(httpRouter, "keys", ct.Key{}, keyRepo)

	httpRouter.PUT("/apps/:apps_id/formations/:releases_id", api.PutFormation)
	httpRouter.GET("/apps/:apps_id/formations/:releases_id", api.GetFormation)
	httpRouter.DELETE("/apps/:apps_id/formations/:releases_id", api.DeleteFormation)
	httpRouter.GET("/apps/:apps_id/formations", api.ListFormations)
	httpRouter.GET("/formations", api.GetFormations)

	httpRouter.POST("/apps/:apps_id/jobs", api.RunJob)
	httpRouter.GET("/apps/:apps_id/jobs/:jobs_id", api.GetJob)
	httpRouter.PUT("/apps/:apps_id/jobs/:jobs_id", api.PutJob)
	httpRouter.GET("/apps/:apps_id/jobs", api.ListJobs)
	httpRouter.DELETE("/apps/:apps_id/jobs/:jobs_id", api.KillJob)
	httpRouter.GET("/apps/:apps_id/jobs/:jobs_id/log", api.JobLog)

	httpRouter.PUT("/apps/:apps_id/release", api.SetAppRelease)
	httpRouter.GET("/apps/:apps_id/release", api.GetAppRelease)

	httpRouter.POST("/providers/:providers_id/resources", api.ProvisionResource)
	httpRouter.GET("/providers/:providers_id/resources", api.GetProviderResources)
	httpRouter.GET("/providers/:providers_id/resources/:resources_id", api.GetResource)
	httpRouter.PUT("/providers/:providers_id/resources/:resources_id", api.PutResource)
	httpRouter.GET("/apps/:apps_id/resources", api.GetAppResources)

	httpRouter.POST("/apps/:apps_id/routes", api.CreateRoute)
	httpRouter.GET("/apps/:apps_id/routes", api.GetRouteList)
	httpRouter.GET("/apps/:apps_id/routes/:routes_type/:routes_id", api.GetRoute)
	httpRouter.DELETE("/apps/:apps_id/routes/:routes_type/:routes_id", api.DeleteRoute)

	return muxHandler(httpRouter, c.key)
}

func muxHandler(main http.Handler, authKey string) http.Handler {
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
		if len(password) != len(authKey) || subtle.ConstantTimeCompare([]byte(password), []byte(authKey)) != 1 {
			w.WriteHeader(401)
			return
		}
		main.ServeHTTP(w, r)
	})
}

type controllerAPI struct {
	appRepo         *AppRepo
	releaseRepo     *ReleaseRepo
	providerRepo    *ProviderRepo
	formationRepo   *FormationRepo
	artifactRepo    *ArtifactRepo
	jobRepo         *JobRepo
	resourceRepo    *ResourceRepo
	clusterClient   clusterClient
	discoverdClient resource.DiscoverdClient
	routerc         routerc.Client
}

func (c *controllerAPI) getApp(params httprouter.Params) (*ct.App, error) {
	data, err := c.appRepo.Get(params.ByName("apps_id"))
	if err != nil {
		return nil, err
	}
	app, _ := data.(*ct.App)
	return app, nil
}

func (c *controllerAPI) getRelease(params httprouter.Params) (*ct.Release, error) {
	data, err := c.releaseRepo.Get(params.ByName("releases_id"))
	if err != nil {
		return nil, err
	}
	release, _ := data.(*ct.Release)
	return release, nil
}

func (c *controllerAPI) getProvider(params httprouter.Params) (*ct.Provider, error) {
	data, err := c.providerRepo.Get(params.ByName("providers_id"))
	if err != nil {
		return nil, err
	}
	provider, _ := data.(*ct.Provider)
	return provider, nil
}

func routeParentRef(appID string) string {
	return "controller/apps/" + appID
}

func routeID(params httprouter.Params) string {
	return params.ByName("routes_type") + "/" + params.ByName("routes_id")
}

func (c *controllerAPI) getRoute(appID string, params httprouter.Params) (*router.Route, error) {
	route, err := c.routerc.GetRoute(routeID(params))
	if err == routerc.ErrNotFound || err == nil && route.ParentRef != routeParentRef(appID) {
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
