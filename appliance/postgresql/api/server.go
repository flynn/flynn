package main

import (
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/go-martini/martini"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/martini-contrib/render"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/postgres"
	"github.com/flynn/flynn/pkg/random"
)

var serviceName = os.Getenv("FLYNN_POSTGRES")

func init() {
	if serviceName == "" {
		serviceName = "pg"
	}
}

func main() {
	username, password := postgres.Wait(serviceName)
	db, err := postgres.Open(serviceName, fmt.Sprintf("dbname=postgres user=%s password=%s", username, password))
	if err != nil {
		log.Fatal(err)
	}

	r := martini.NewRouter()
	m := martini.New()
	m.Use(martini.Logger())
	m.Use(martini.Recovery())
	m.Use(render.Renderer())
	m.Action(r.Handle)
	m.Map(db)

	r.Post("/databases", createDatabase)
	r.Get("/ping", ping)

	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}
	addr := ":" + port

	if err := discoverd.Register(serviceName+"-api", addr); err != nil {
		log.Fatal(err)
	}

	log.Fatal(http.ListenAndServe(addr, m))
}

type resource struct {
	ID  string            `json:"id"`
	Env map[string]string `json:"env"`
}

func createDatabase(db *postgres.DB, r render.Render) {
	username, password, database := random.Hex(16), random.Hex(16), random.Hex(16)

	if err := db.Exec(fmt.Sprintf(`CREATE USER "%s" WITH PASSWORD '%s'`, username, password)); err != nil {
		log.Println(err)
		r.JSON(500, struct{}{})
		return
	}
	if err := db.Exec(fmt.Sprintf(`CREATE DATABASE "%s" WITH OWNER = "%s"`, database, username)); err != nil {
		db.Exec(fmt.Sprintf(`DROP USER "%s"`, username))
		log.Println(err)
		r.JSON(500, struct{}{})
		return
	}

	r.JSON(200, &resource{
		ID: fmt.Sprintf("/databases/%s:%s", username, database),
		Env: map[string]string{
			"FLYNN_POSTGRES": serviceName,
			"PGUSER":         username,
			"PGPASSWORD":     password,
			"PGDATABASE":     database,
		},
	})
}

func ping(db *postgres.DB, w http.ResponseWriter) {
	if err := db.Exec("SELECT 1"); err != nil {
		log.Println(err)
		w.WriteHeader(500)
		return
	}
	w.WriteHeader(200)
}
