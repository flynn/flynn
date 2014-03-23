package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"

	"github.com/codegangsta/martini"
	"github.com/flynn/go-discoverd"
	"github.com/flynn/go-flynn/postgres"
	"github.com/martini-contrib/render"
)

var serviceName = os.Getenv("PGSERVICE")

func init() {
	if serviceName == "" {
		serviceName = "pg"
	}
}

func main() {
	username, password := waitForPostgres(serviceName)
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

	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}
	http.ListenAndServe(":"+port, m)
}

func randomID() string {
	id := make([]byte, 16)
	_, err := io.ReadFull(rand.Reader, id)
	if err != nil {
		panic(err) // This shouldn't ever happen, right?
	}
	return hex.EncodeToString(id)
}

func waitForPostgres(name string) (string, string) {
	set, err := discoverd.NewServiceSet(name)
	if err != nil {
		log.Fatal(err)
	}
	defer set.Close()
	ch := set.Watch(true)
	for u := range ch {
		fmt.Printf("%#v\n", u)
		l := set.Leader()
		if l == nil {
			continue
		}
		if u.Online && u.Addr == l.Addr && u.Attrs["up"] == "true" && u.Attrs["username"] != "" && u.Attrs["password"] != "" {
			return u.Attrs["username"], u.Attrs["password"]
		}
	}
	panic("discoverd disconnected before postgres came up")
}

type resource struct {
	ID  string            `json:"id"`
	Env map[string]string `json:"env"`
}

func createDatabase(db *postgres.DB, r render.Render) {
	username, password, database := randomID(), randomID(), randomID()

	if _, err := db.Exec(fmt.Sprintf(`CREATE USER "%s" WITH PASSWORD '%s'`, username, password)); err != nil {
		log.Println(err)
		r.JSON(500, struct{}{})
		return
	}
	if _, err := db.Exec(fmt.Sprintf(`CREATE DATABASE "%s" WITH OWNER = "%s"`, database, username)); err != nil {
		db.Exec(fmt.Sprintf(`DROP USER "%s"`, username))
		log.Println(err)
		r.JSON(500, struct{}{})
		return
	}

	r.JSON(200, &resource{
		ID: fmt.Sprintf("/databases/%s:%s", username, database),
		Env: map[string]string{
			"PGSERVICE":  serviceName,
			"PGUSER":     username,
			"PGPASSWORD": password,
			"PGDATABASE": database,
		},
	})
}
