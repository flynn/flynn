package main

import (
	"fmt"
	"net/http"
	"os"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/julienschmidt/httprouter"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/postgres"
	"github.com/flynn/flynn/pkg/random"
	"github.com/flynn/flynn/pkg/shutdown"
)

var serviceName = os.Getenv("FLYNN_POSTGRES")
var serviceHost string

func init() {
	if serviceName == "" {
		serviceName = "postgres"
	}
	serviceHost = fmt.Sprintf("leader.%s.discoverd", serviceName)
}

func main() {
	defer shutdown.Exit()

	db := postgres.Wait(serviceName, fmt.Sprintf("dbname=postgres user=flynn password=%s", os.Getenv("PGPASSWORD")))
	api := &pgAPI{db}

	router := httprouter.New()
	router.POST("/databases", api.createDatabase)
	router.GET("/ping", api.ping)

	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}
	addr := ":" + port

	hb, err := discoverd.AddServiceAndRegister(serviceName+"-api", addr)
	if err != nil {
		shutdown.Fatal(err)
	}
	shutdown.BeforeExit(func() { hb.Close() })

	handler := httphelper.ContextInjector(serviceName+"-api", httphelper.NewRequestLogger(router))
	shutdown.Fatal(http.ListenAndServe(addr, handler))
}

type resource struct {
	ID  string            `json:"id"`
	Env map[string]string `json:"env"`
}

type pgAPI struct {
	db *postgres.DB
}

func (p *pgAPI) createDatabase(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	username, password, database := random.Hex(16), random.Hex(16), random.Hex(16)

	if err := p.db.Exec(fmt.Sprintf(`CREATE USER "%s" WITH PASSWORD '%s'`, username, password)); err != nil {
		httphelper.Error(w, err)
		return
	}
	if err := p.db.Exec(fmt.Sprintf(`CREATE DATABASE "%s" WITH OWNER = "%s"`, database, username)); err != nil {
		p.db.Exec(fmt.Sprintf(`DROP USER "%s"`, username))
		httphelper.Error(w, err)
		return
	}

	httphelper.JSON(w, 200, &resource{
		ID: fmt.Sprintf("/databases/%s:%s", username, database),
		Env: map[string]string{
			"FLYNN_POSTGRES": serviceName,
			"PGHOST":         serviceHost,
			"PGUSER":         username,
			"PGPASSWORD":     password,
			"PGDATABASE":     database,
		},
	})
}

func (p *pgAPI) ping(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	if err := p.db.Exec("SELECT 1"); err != nil {
		httphelper.Error(w, err)
		return
	}
	w.WriteHeader(200)
}
