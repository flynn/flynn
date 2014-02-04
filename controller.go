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
	m.Map(NewArtifactRepo())
	m.Action(r.Handle)

	r.Post("/apps", binding.Bind(App{}), createApp)
	r.Get("/apps/:app_id", getAppMiddleware, getApp)

	r.Post("/artifacts", binding.Bind(Artifact{}), createArtifact)
	r.Get("/artifacts/:artifact_id", getArtifactMiddleware, getArtifact)

	return m
}

type Repository interface {
	Add(interface{}) error
	Get(string) (interface{}, error)
}

// POST /apps
func createApp(app App, repo *AppRepo, r render.Render) {
	if err := repo.Add(&app); err != nil {
		// TODO: handle error
	}
	r.JSON(200, &app)
}

func getAppMiddleware(c martini.Context, repo *AppRepo, params martini.Params, w http.ResponseWriter) {
	app, err := repo.Get(params["app_id"])
	if err != nil {
		if err == ErrNotFound {
			w.WriteHeader(404)
			return
		}
		// TODO: 500/log error
	}
	c.Map(app)
}

// GET /apps/:app_id
func getApp(app *App, r render.Render) {
	r.JSON(200, app)
}

// POST /artifacts
func createArtifact(artifact Artifact, repo *ArtifactRepo, r render.Render) {
	if err := repo.Add(&artifact); err != nil {
		// TODO: handle error
	}
	r.JSON(200, &artifact)
}

func getArtifactMiddleware(c martini.Context, repo *ArtifactRepo, params martini.Params, w http.ResponseWriter) {
	artifact, err := repo.Get(params["artifact_id"])
	if err != nil {
		if err == ErrNotFound {
			w.WriteHeader(404)
			return
		}
		// TODO: 500/log error
	}
	c.Map(artifact)
}

// GET /artifacts/:artifact_id
func getArtifact(artifact *Artifact, r render.Render) {
	r.JSON(200, artifact)
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
