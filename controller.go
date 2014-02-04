package main

import (
	"net/http"

	"github.com/codegangsta/martini"
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
	m.Action(r.Handle)

	appRepo := NewAppRepo()
	artifactRepo := NewArtifactRepo()
	m.Map(appRepo)
	m.Map(artifactRepo)

	crud("apps", App{}, appRepo, r)
	crud("artifacts", Artifact{}, artifactRepo, r)

	return m
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
