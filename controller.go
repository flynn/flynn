package main

import (
	"log"
	"net/http"
	"os"

	ct "github.com/flynn/flynn-controller/types"
	"github.com/flynn/go-flynn/resource"
	"github.com/flynn/go-sql"
	"github.com/flynn/rpcplus"
	"github.com/go-martini/martini"
	"github.com/martini-contrib/binding"
	"github.com/martini-contrib/render"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}
	handler, _ := appHandler(nil, nil)
	http.ListenAndServe(":"+port, handler)
}

func appHandler(db *sql.DB, cc clusterClient) (http.Handler, *martini.Martini) {
	r := martini.NewRouter()
	m := martini.New()
	m.Use(martini.Logger())
	m.Use(martini.Recovery())
	m.Use(render.Renderer())
	m.Action(r.Handle)

	d := NewDB(db)

	providerRepo := NewProviderRepo(d)
	keyRepo := NewKeyRepo(d)
	resourceRepo := NewResourceRepo(d)
	appRepo := NewAppRepo(d)
	artifactRepo := NewArtifactRepo(d)
	releaseRepo := NewReleaseRepo(d)
	formationRepo := NewFormationRepo(d, appRepo, releaseRepo, artifactRepo)
	m.Map(resourceRepo)
	m.Map(appRepo)
	m.Map(artifactRepo)
	m.Map(releaseRepo)
	m.Map(formationRepo)
	m.MapTo(cc, (*clusterClient)(nil))

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
	r.Get("/apps/:apps_id/resources", getAppMiddleware, getAppResources)

	return rpcMuxHandler(m, rpcHandler(formationRepo)), m
}

func rpcMuxHandler(main http.Handler, rpch http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
