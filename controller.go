package main

import (
	"net/http"
	"os"

	"github.com/codegangsta/martini"
	ct "github.com/flynn/flynn-controller/types"
	"github.com/flynn/rpcplus"
	"github.com/martini-contrib/binding"
	"github.com/martini-contrib/render"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}
	http.ListenAndServe(":"+port, appHandler(nil))
}

func appHandler(cc clusterClient) http.Handler {
	r := martini.NewRouter()
	m := martini.New()
	m.Use(martini.Logger())
	m.Use(martini.Recovery())
	m.Use(render.Renderer())
	m.Action(r.Handle)

	keyRepo := NewKeyRepo()
	appRepo := NewAppRepo()
	artifactRepo := NewArtifactRepo()
	releaseRepo := NewReleaseRepo(artifactRepo)
	formationRepo := NewFormationRepo(appRepo, releaseRepo, artifactRepo)
	m.Map(appRepo)
	m.Map(artifactRepo)
	m.Map(releaseRepo)
	m.Map(formationRepo)
	m.MapTo(cc, (*clusterClient)(nil))

	getAppMiddleware := crud("apps", ct.App{}, appRepo, r)
	getReleaseMiddleware := crud("releases", ct.Release{}, releaseRepo, r)
	crud("artifacts", ct.Artifact{}, artifactRepo, r)
	crud("keys", ct.Key{}, keyRepo, r)

	r.Put("/apps/:apps_id/formations/:releases_id", getAppMiddleware, getReleaseMiddleware, binding.Bind(ct.Formation{}), putFormation)
	r.Get("/apps/:apps_id/formations/:releases_id", getFormationMiddleware, getFormation)
	r.Delete("/apps/:apps_id/formations/:releases_id", getFormationMiddleware, deleteFormation)
	r.Get("/apps/:apps_id/formations", getAppMiddleware, listFormations)

	r.Get("/apps/:apps_id/jobs", getAppMiddleware, jobList)
	r.Delete("/apps/:apps_id/jobs/:job_id", getAppMiddleware, connectHostMiddleware, killJob)
	r.Get("/apps/:apps_id/jobs/:job_id/logs", getAppMiddleware, connectHostMiddleware, jobLogs)

	return rpcMuxHandler(m, rpcHandler(formationRepo))
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
		// TODO: 500/log error
	}
	r.JSON(200, &formation)
}

func getFormationMiddleware(c martini.Context, params martini.Params, repo *FormationRepo, w http.ResponseWriter) {
	formation, err := repo.Get(params["apps_id"], params["releases_id"])
	if err != nil {
		if err == ErrNotFound {
			w.WriteHeader(404)
			return
		}
		// TODO: 500/log error
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
		// TODO: 500/log error
		return
	}
	w.WriteHeader(200)
}

func listFormations(app *ct.App, repo *FormationRepo, r render.Render) {
	list, err := repo.List(app.ID)
	if err != nil {
		// TODO: 500/log error
		return
	}
	r.JSON(200, list)
}

/*
created
starting
up
stopping
down
(bounce)?
*/
