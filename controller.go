package main

import (
	"net/http"

	"github.com/codegangsta/martini"
	"github.com/codegangsta/martini-contrib/binding"
	"github.com/codegangsta/martini-contrib/render"
	"github.com/flynn/rpcplus"
)

func main() {
}

func appHandler() http.Handler {
	r := martini.NewRouter()
	m := martini.New()
	m.Use(martini.Logger())
	m.Use(martini.Recovery())
	m.Use(render.Renderer())
	m.Action(r.Handle)

	appRepo := NewAppRepo()
	artifactRepo := NewArtifactRepo()
	releaseRepo := NewReleaseRepo(artifactRepo)
	formationRepo := NewFormationRepo(appRepo, releaseRepo)
	m.Map(appRepo)
	m.Map(artifactRepo)
	m.Map(releaseRepo)
	m.Map(formationRepo)

	getAppMiddleware := crud("apps", App{}, appRepo, r)
	getReleaseMiddleware := crud("releases", Release{}, releaseRepo, r)
	crud("artifacts", Artifact{}, artifactRepo, r)

	r.Put("/apps/:apps_id/formations/:releases_id", getAppMiddleware, getReleaseMiddleware, binding.Bind(Formation{}), putFormation)
	r.Get("/apps/:apps_id/formations/:releases_id", getFormationMiddleware, getFormation)
	r.Delete("/apps/:apps_id/formations/:releases_id", getFormationMiddleware, deleteFormation)

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

func putFormation(formation Formation, app *App, release *Release, repo *FormationRepo, r render.Render) {
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

func getFormation(formation *Formation, r render.Render) {
	r.JSON(200, formation)
}

func deleteFormation(formation *Formation, repo *FormationRepo, w http.ResponseWriter) {
	err := repo.Remove(formation.AppID, formation.ReleaseID)
	if err != nil {
		// TODO: 500/log error
		return
	}
	w.WriteHeader(200)
}

/*
created
starting
up
stopping
down
(bounce)?
*/
