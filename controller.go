package main

import (
	"net/http"

	"github.com/codegangsta/martini"
	"github.com/codegangsta/martini-contrib/render"
)

func main() {
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
	m.Action(r.Handle)

	appRepo := NewAppRepo()
	artifactRepo := NewArtifactRepo()
	releaseRepo := NewReleaseRepo(artifactRepo)
	m.Map(appRepo)
	m.Map(artifactRepo)
	m.Map(releaseRepo)

	crud("apps", App{}, appRepo, r)
	crud("artifacts", Artifact{}, artifactRepo, r)
	crud("releases", Release{}, releaseRepo, r)

	return m
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
