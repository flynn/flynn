package main

import (
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"strings"

	_ "github.com/flynn/flynn/Godeps/_workspace/src/github.com/go-sql-driver/mysql"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/julienschmidt/httprouter"
	"github.com/flynn/flynn/Godeps/_workspace/src/golang.org/x/net/context"
	"github.com/flynn/flynn/appliance/mariadb"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/random"
	"github.com/flynn/flynn/pkg/resource"
	"github.com/flynn/flynn/pkg/shutdown"
)

var serviceName = os.Getenv("FLYNN_MYSQL")
var serviceHost string

func init() {
	if serviceName == "" {
		serviceName = "mariadb"
	}
	serviceHost = fmt.Sprintf("leader.%s.discoverd", serviceName)
}

func main() {
	defer shutdown.Exit()

	dsn := &mariadb.DSN{
		Host:     serviceHost + ":3306",
		User:     "flynn",
		Password: os.Getenv("MYSQL_PWD"),
		Database: "mysql",
	}
	db, err := sql.Open("mysql", dsn.String())
	api := &API{db}

	router := httprouter.New()
	router.POST("/databases", httphelper.WrapHandler(api.createDatabase))
	router.DELETE("/databases", httphelper.WrapHandler(api.dropDatabase))
	router.GET("/ping", httphelper.WrapHandler(api.ping))

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

type API struct {
	db *sql.DB
}

func (a *API) createDatabase(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	username, password, database := random.Hex(16), random.Hex(16), random.Hex(16)

	if _, err := a.db.Exec(fmt.Sprintf(`CREATE USER '%s'@'%%' IDENTIFIED BY '%s'`, username, password)); err != nil {
		httphelper.Error(w, err)
		return
	}
	if _, err := a.db.Exec(fmt.Sprintf(`CREATE DATABASE %s`, database)); err != nil {
		a.db.Exec(fmt.Sprintf(`DROP USER "%s"`, username))
		httphelper.Error(w, err)
		return
	}
	if _, err := a.db.Exec(fmt.Sprintf(`GRANT ALL ON %s.* TO '%s'@'%%'`, database, username)); err != nil {
		a.db.Exec(fmt.Sprintf(`DROP DATABASE "%s"`, database))
		a.db.Exec(fmt.Sprintf(`DROP USER "%s"`, username))
		httphelper.Error(w, err)
		return
	}

	url := fmt.Sprintf("mysql://%s:%s@%s:3306/%s", username, password, serviceHost, database)
	httphelper.JSON(w, 200, resource.Resource{
		ID: fmt.Sprintf("/databases/%s:%s", username, database),
		Env: map[string]string{
			"FLYNN_MYSQL":    serviceName,
			"MYSQL_HOST":     serviceHost,
			"MYSQL_USER":     username,
			"MYSQL_PWD":      password,
			"MYSQL_DATABASE": database,
			"DATABASE_URL":   url,
		},
	})
}

func (a *API) dropDatabase(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	id := strings.SplitN(strings.TrimPrefix(req.FormValue("id"), "/databases/"), ":", 2)
	if len(id) != 2 || id[1] == "" {
		httphelper.ValidationError(w, "id", "is invalid")
		return
	}

	if _, err := a.db.Exec(fmt.Sprintf(`DROP DATABASE "%s"`, id[1])); err != nil {
		httphelper.Error(w, err)
		return
	}

	if _, err := a.db.Exec(fmt.Sprintf(`DROP USER "%s"`, id[0])); err != nil {
		httphelper.Error(w, err)
		return
	}

	w.WriteHeader(200)
}

func (a *API) ping(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	if _, err := a.db.Exec("SELECT 1"); err != nil {
		httphelper.Error(w, err)
		return
	}
	w.WriteHeader(200)
}
