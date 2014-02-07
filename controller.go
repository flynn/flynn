package main

import (
	"fmt"
	"net/http"

	"github.com/codegangsta/martini"
	"github.com/codegangsta/martini-contrib/binding"
	"github.com/codegangsta/martini-contrib/render"
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
	formationRepo := NewFormationRepo()
	m.Map(appRepo)
	m.Map(artifactRepo)
	m.Map(releaseRepo)
	m.Map(formationRepo)

	getAppMiddleware := crud("apps", App{}, appRepo, r)
	getReleaseMiddleware := crud("releases", Release{}, releaseRepo, r)
	crud("artifacts", Artifact{}, artifactRepo, r)

	r.Put("/apps/:apps_id/formations/:releases_id", func() { fmt.Println("BOOM") }, getAppMiddleware, getReleaseMiddleware, binding.Bind(Formation{}), putFormation)
	r.Get("/apps/:apps_id/formations/:releases_id", getFormationMiddleware, getFormation)

	return m
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

func deleteFormation() {
}

/*
created
starting
up
stopping
down
(bounce)?
*/
