package main

import (
	"net/http"

	"github.com/codegangsta/martini"
	"github.com/codegangsta/martini-contrib/binding"
	"github.com/codegangsta/martini-contrib/render"
)

func main() {
	// create artifact
	// create release
	// create formation
	// update formation
	// delete formation
}

func appHandler() http.Handler {
	r := martini.NewRouter()
	m := martini.New()
	m.Use(martini.Logger())
	m.Use(martini.Recovery())
	m.Use(render.Renderer())
	m.Map(NewAppRepo())
	m.Action(r.Handle)

	r.Post("/apps", binding.Bind(App{}), createApp)
	r.Get("/apps/:app_id", getAppMiddleware, getApp)

	return m
}

// POST /apps
func createApp(app App, repo *AppRepo, r render.Render) {
	if err := repo.Create(&app); err != nil {
		// TODO: handle error
	}
	r.JSON(200, &app)
}

func getAppMiddleware(c martini.Context, repo *AppRepo, params martini.Params, w http.ResponseWriter) {
	app := repo.Get(params["app_id"])
	if app == nil {
		w.WriteHeader(404)
		return
	}
	c.Map(app)
}

// GET /apps/:app_id
func getApp(app *App, r render.Render) {
	r.JSON(200, app)
}

func createArtifact() {
	// validate
	// assign id
	// save to etcd
	// return
}

func createRelease() {
	// validate
	// assign id
	// save to etcd
	// return
}

func createFormation() {
	// lookup app
	// lookup release
	// validate
	// assign id
	// save to etcd
	// return
}

func updateFormation() {
	// lookup formation
	// lookup release
	// validate
	// save to etcd
	// return
}

func deleteFormation() {
	// save to etcd
}

/*
created
starting
up
stopping
down
(bounce)?
*/
