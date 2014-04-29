package main

import (
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	ct "github.com/flynn/flynn-controller/types"
	"github.com/flynn/go-discoverd"
	"github.com/flynn/go-flynn/cluster"
	"github.com/flynn/go-flynn/postgres"
	"github.com/flynn/go-flynn/resource"
	"github.com/flynn/go-sql"
	"github.com/flynn/rpcplus"
	strowgerc "github.com/flynn/strowger/client"
	"github.com/flynn/strowger/types"
	"github.com/go-martini/martini"
	"github.com/martini-contrib/binding"
	"github.com/martini-contrib/render"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}
	addr := ":" + port

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

	sc, err := strowgerc.New()
	if err != nil {
		log.Fatal(err)
	}

	if err := discoverd.Register("flynn-controller", addr); err != nil {
		log.Fatal(err)
	}

	handler, _ := appHandler(handlerConfig{db: db, cc: cc, sc: sc, dc: discoverd.DefaultClient, key: os.Getenv("AUTH_KEY")})
	log.Fatal(http.ListenAndServe(addr, handler))
}

type dbWrapper interface {
	Database() *sql.DB
	DSN() string
	Close() error
}

type handlerConfig struct {
	db  dbWrapper
	cc  clusterClient
	sc  strowgerc.Client
	dc  *discoverd.Client
	key string
}

type ResponseHelper interface {
	Error(error)
	JSON(int, interface{})
}

type responseHelper struct {
	render.Render
}

func (r *responseHelper) Error(err error) {
	switch err.(type) {
	case ct.ValidationError:
		r.JSON(400, err)
	case *json.SyntaxError, *json.UnmarshalTypeError:
		r.JSON(400, ct.ValidationError{Message: "The provided JSON input is invalid"})
	default:
		log.Println(err)
		r.JSON(500, struct{}{})
	}
}

func responseHelperHandler(c martini.Context, r render.Render) {
	c.MapTo(&responseHelper{r}, (*ResponseHelper)(nil))
}

func appHandler(c handlerConfig) (http.Handler, *martini.Martini) {
	r := martini.NewRouter()
	m := martini.New()
	m.Use(martini.Logger())
	m.Use(martini.Recovery())
	m.Use(render.Renderer())
	m.Use(responseHelperHandler)
	m.Action(r.Handle)

	d := NewDB(c.db)

	providerRepo := NewProviderRepo(d)
	keyRepo := NewKeyRepo(d)
	resourceRepo := NewResourceRepo(d)
	appRepo := NewAppRepo(d, os.Getenv("DEFAULT_ROUTE_DOMAIN"), c.sc)
	artifactRepo := NewArtifactRepo(d)
	releaseRepo := NewReleaseRepo(d)
	formationRepo := NewFormationRepo(d, appRepo, releaseRepo, artifactRepo)
	m.Map(resourceRepo)
	m.Map(appRepo)
	m.Map(artifactRepo)
	m.Map(releaseRepo)
	m.Map(formationRepo)
	m.Map(c.dc)
	m.MapTo(c.cc, (*clusterClient)(nil))
	m.MapTo(c.sc, (*strowgerc.Client)(nil))
	m.MapTo(c.dc, (*resource.DiscoverdClient)(nil))

	getAppMiddleware := crud("apps", ct.App{}, appRepo, r)
	getReleaseMiddleware := crud("releases", ct.Release{}, releaseRepo, r)
	getProviderMiddleware := crud("providers", ct.Provider{}, providerRepo, r)
	crud("artifacts", ct.Artifact{}, artifactRepo, r)
	crud("keys", ct.Key{}, keyRepo, r)

	r.Put("/apps/:apps_id/formations/:releases_id", getAppMiddleware, getReleaseMiddleware, binding.Bind(ct.Formation{}), putFormation)
	r.Get("/apps/:apps_id/formations/:releases_id", getAppMiddleware, getFormationMiddleware, getFormation)
	r.Delete("/apps/:apps_id/formations/:releases_id", getAppMiddleware, getFormationMiddleware, deleteFormation)
	r.Get("/apps/:apps_id/formations", getAppMiddleware, listFormations)

	r.Post("/apps/:apps_id/jobs", getAppMiddleware, binding.Bind(ct.NewJob{}), runJob)
	r.Get("/apps/:apps_id/jobs", getAppMiddleware, jobList)
	r.Delete("/apps/:apps_id/jobs/:jobs_id", getAppMiddleware, connectHostMiddleware, killJob)
	r.Get("/apps/:apps_id/jobs/:jobs_id/log", getAppMiddleware, connectHostMiddleware, jobLog)

	r.Put("/apps/:apps_id/release", getAppMiddleware, binding.Bind(releaseID{}), setAppRelease)
	r.Get("/apps/:apps_id/release", getAppMiddleware, getAppRelease)

	r.Post("/providers/:providers_id/resources", getProviderMiddleware, binding.Bind(ct.ResourceReq{}), resourceServerMiddleware, provisionResource)
	r.Get("/providers/:providers_id/resources", getProviderMiddleware, getProviderResources)
	r.Get("/providers/:providers_id/resources/:resources_id", getProviderMiddleware, getResourceMiddleware, getResource)
	r.Put("/providers/:providers_id/resources/:resources_id", getProviderMiddleware, binding.Bind(ct.Resource{}), putResource)
	r.Get("/apps/:apps_id/resources", getAppMiddleware, getAppResources)

	r.Post("/apps/:apps_id/routes", getAppMiddleware, binding.Bind(strowger.Route{}), createRoute)
	r.Get("/apps/:apps_id/routes", getAppMiddleware, getRouteList)
	r.Get("/apps/:apps_id/routes/:routes_type/:routes_id", getAppMiddleware, getRouteMiddleware, getRoute)
	r.Delete("/apps/:apps_id/routes/:routes_type/:routes_id", getAppMiddleware, getRouteMiddleware, deleteRoute)

	return rpcMuxHandler(m, rpcHandler(formationRepo), c.key), m
}

func rpcMuxHandler(main http.Handler, rpch http.Handler, authKey string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ping" {
			w.WriteHeader(200)
			return
		}
		_, password, _ := parseBasicAuth(r.Header)
		if len(password) != len(authKey) || subtle.ConstantTimeCompare([]byte(password), []byte(authKey)) != 1 {
			w.WriteHeader(401)
			return
		}
		if r.URL.Path == rpcplus.DefaultRPCPath {
			rpch.ServeHTTP(w, r)
		} else {
			main.ServeHTTP(w, r)
		}
	})
}

func putFormation(formation ct.Formation, app *ct.App, release *ct.Release, repo *FormationRepo, r render.Render) {
	formation.AppID = app.ID
	formation.ReleaseID = release.ID
	err := repo.Add(&formation)
	if app.Protected {
		for typ := range release.Processes {
			if formation.Processes[typ] == 0 {
				r.JSON(400, struct{}{})
				return
			}
		}
	}
	if err != nil {
		log.Println(err)
		r.JSON(500, struct{}{})
		return
	}
	r.JSON(200, &formation)
}

func getFormationMiddleware(c martini.Context, app *ct.App, params martini.Params, repo *FormationRepo, w http.ResponseWriter) {
	formation, err := repo.Get(app.ID, params["releases_id"])
	if err != nil {
		if err == ErrNotFound {
			w.WriteHeader(404)
			return
		}
		log.Println(err)
		w.WriteHeader(500)
		return
	}
	c.Map(formation)
}

func getFormation(formation *ct.Formation, r render.Render) {
	r.JSON(200, formation)
}

func deleteFormation(formation *ct.Formation, repo *FormationRepo, w http.ResponseWriter) {
	err := repo.Remove(formation.AppID, formation.ReleaseID)
	if err != nil {
		log.Println(err)
		w.WriteHeader(500)
		return
	}
	w.WriteHeader(200)
}

func listFormations(app *ct.App, repo *FormationRepo, r render.Render) {
	list, err := repo.List(app.ID)
	if err != nil {
		log.Println(err)
		r.JSON(500, struct{}{})
		return
	}
	r.JSON(200, list)
}

type releaseID struct {
	ID string `json:"id"`
}

func setAppRelease(app *ct.App, rid releaseID, apps *AppRepo, releases *ReleaseRepo, formations *FormationRepo, r render.Render) {
	rel, err := releases.Get(rid.ID)
	if err != nil {
		log.Println(err)
		r.JSON(500, struct{}{})
		return
	}
	release := rel.(*ct.Release)
	apps.SetRelease(app.ID, release.ID)

	// TODO: use transaction/lock
	fs, err := formations.List(app.ID)
	if err != nil {
		log.Println(err)
		r.JSON(500, struct{}{})
		return
	}
	if len(fs) == 1 && fs[0].ReleaseID != release.ID {
		if err := formations.Add(&ct.Formation{
			AppID:     app.ID,
			ReleaseID: release.ID,
			Processes: fs[0].Processes,
		}); err != nil {
			log.Println(err)
			r.JSON(500, struct{}{})
			return
		}
		if err := formations.Remove(app.ID, fs[0].ReleaseID); err != nil {
			log.Println(err)
			r.JSON(500, struct{}{})
			return
		}
	}

	r.JSON(200, release)
}

func getAppRelease(app *ct.App, apps *AppRepo, r render.Render, w http.ResponseWriter) {
	release, err := apps.GetRelease(app.ID)
	if err != nil {
		if err == ErrNotFound {
			w.WriteHeader(404)
			return
		}
		log.Println(err)
		w.WriteHeader(500)
		return
	}
	r.JSON(200, release)
}

func resourceServerMiddleware(c martini.Context, p *ct.Provider, dc resource.DiscoverdClient, w http.ResponseWriter) {
	server, err := resource.NewServerWithDiscoverd(p.URL, dc)
	if err != nil {
		log.Println(err)
		w.WriteHeader(500)
		return
	}
	c.Map(server)
	c.Next()
	server.Close()
}

func putResource(p *ct.Provider, params martini.Params, resource ct.Resource, repo *ResourceRepo, r render.Render) {
	resource.ID = params["resources_id"]
	resource.ProviderID = p.ID
	if err := repo.Add(&resource); err != nil {
		log.Println(err)
		r.JSON(500, struct{}{})
		return
	}
	r.JSON(200, &resource)
}

func provisionResource(rs *resource.Server, p *ct.Provider, req ct.ResourceReq, repo *ResourceRepo, r render.Render) {
	var config []byte
	if req.Config != nil {
		config = *req.Config
	} else {
		config = []byte(`{}`)
	}
	data, err := rs.Provision(config)
	if err != nil {
		log.Println(err)
		r.JSON(500, struct{}{})
		return
	}

	res := &ct.Resource{
		ProviderID: p.ID,
		ExternalID: data.ID,
		Env:        data.Env,
		Apps:       req.Apps,
	}
	if err := repo.Add(res); err != nil {
		// TODO: attempt to "rollback" provisioning
		log.Println(err)
		r.JSON(500, struct{}{})
		return
	}
	r.JSON(200, res)
}

func getResourceMiddleware(c martini.Context, params martini.Params, repo *ResourceRepo, w http.ResponseWriter) {
	resource, err := repo.Get(params["resources_id"])
	if err != nil {
		if err == ErrNotFound {
			w.WriteHeader(404)
			return
		}
		log.Println(err)
		w.WriteHeader(500)
		return
	}
	c.Map(resource)
}

func getResource(resource *ct.Resource, r render.Render) {
	r.JSON(200, resource)
}

func getProviderResources(p *ct.Provider, repo *ResourceRepo, r render.Render) {
	res, err := repo.ProviderList(p.ID)
	if err != nil {
		log.Println(err)
		r.JSON(500, struct{}{})
		return
	}
	r.JSON(200, res)
}

func getAppResources(app *ct.App, repo *ResourceRepo, r render.Render) {
	res, err := repo.AppList(app.ID)
	if err != nil {
		log.Println(err)
		r.JSON(500, struct{}{})
		return
	}
	r.JSON(200, res)
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
		return "", "", errors.New("failed to parse base64 basic credenti als")
	}

	s = strings.SplitN(string(c), ":", 2)
	if len(s) != 2 {
		return "", "", errors.New("failed to parse basic credentials")
	}

	return s[0], s[1], nil
}
