package main

import (
	"database/sql"
	"log"
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
	http.ListenAndServe(":"+port, appHandler(nil, nil))
}

func appHandler(db *sql.DB, cc clusterClient) http.Handler {
	r := martini.NewRouter()
	m := martini.New()
	m.Use(martini.Logger())
	m.Use(martini.Recovery())
	m.Use(render.Renderer())
	m.Action(r.Handle)

	d := NewDB(db)

	keyRepo := NewKeyRepo(d)
	appRepo := NewAppRepo(d)
	artifactRepo := NewArtifactRepo(d)
	releaseRepo := NewReleaseRepo(d)
	formationRepo := NewFormationRepo(d, appRepo, releaseRepo, artifactRepo)
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
	r.Get("/apps/:apps_id/formations/:releases_id", getAppMiddleware, getFormationMiddleware, getFormation)
	r.Delete("/apps/:apps_id/formations/:releases_id", getAppMiddleware, getFormationMiddleware, deleteFormation)
	r.Get("/apps/:apps_id/formations", getAppMiddleware, listFormations)

	r.Post("/apps/:apps_id/jobs", getAppMiddleware, binding.Bind(ct.NewJob{}), runJob)
	r.Get("/apps/:apps_id/jobs", getAppMiddleware, jobList)
	r.Delete("/apps/:apps_id/jobs/:job_id", getAppMiddleware, connectHostMiddleware, killJob)
	r.Get("/apps/:apps_id/jobs/:job_id/log", getAppMiddleware, connectHostMiddleware, jobLog)

	r.Put("/apps/:apps_id/release", getAppMiddleware, binding.Bind(releaseID{}), setAppRelease)
	r.Get("/apps/:apps_id/release", getAppMiddleware, getAppRelease)

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
