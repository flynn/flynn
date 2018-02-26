package main

import (
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/flynn/flynn/appliance/mariadb"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/random"
	"github.com/flynn/flynn/pkg/resource"
	"github.com/flynn/flynn/pkg/shutdown"
	sirenia "github.com/flynn/flynn/pkg/sirenia/client"
	"github.com/flynn/flynn/pkg/sirenia/scale"
	_ "github.com/go-sql-driver/mysql"
	"github.com/julienschmidt/httprouter"
	"golang.org/x/net/context"
	"github.com/inconshreveable/log15"
)

var serviceName = os.Getenv("FLYNN_MYSQL")
var app = os.Getenv("FLYNN_APP_ID")
var controllerKey = os.Getenv("CONTROLLER_KEY")
var singleton = os.Getenv("SINGLETON")
var serviceHost string

func init() {
	if serviceName == "" {
		serviceName = "mariadb"
	}
	serviceHost = fmt.Sprintf("leader.%s.discoverd", serviceName)
}

func main() {
	defer shutdown.Exit()

	api := &API{}

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
	mtx      sync.Mutex
	scaledUp bool
}

func (a *API) createDatabase(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	// Ensure the cluster has been scaled up before attempting to create a database.
	if err := a.scaleUp(); err != nil {
		httphelper.Error(w, err)
		return
	}

	db, err := a.connect()
	if err != nil {
		httphelper.Error(w, err)
		return
	}
	defer db.Close()

	username, password, database := random.Hex(16), random.Hex(16), random.Hex(16)
	if _, err := db.Exec(fmt.Sprintf("CREATE USER '%s'@'%%' IDENTIFIED BY '%s'", username, password)); err != nil {
		httphelper.Error(w, err)
		return
	}
	if _, err := db.Exec(fmt.Sprintf("CREATE DATABASE `%s`", database)); err != nil {
		db.Exec(fmt.Sprintf("DROP USER '%s'", username))
		httphelper.Error(w, err)
		return
	}
	if _, err := db.Exec(fmt.Sprintf("GRANT ALL ON `%s`.* TO '%s'@'%%'", database, username)); err != nil {
		db.Exec(fmt.Sprintf("DROP DATABASE `%s`", database))
		db.Exec(fmt.Sprintf("DROP USER '%s'", username))
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

	db, err := a.connect()
	if err != nil {
		httphelper.Error(w, err)
		return
	}
	defer db.Close()

	if _, err := db.Exec(fmt.Sprintf("DROP DATABASE `%s`", id[1])); err != nil {
		httphelper.Error(w, err)
		return
	}

	if _, err := db.Exec(fmt.Sprintf("DROP USER '%s'", id[0])); err != nil {
		httphelper.Error(w, err)
		return
	}

	w.WriteHeader(200)
}

func (a *API) ping(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	logger := a.logger().New("fn", "ping")

	logger.Info("checking status", "host", serviceHost)
	if status, err := sirenia.NewClient(serviceHost + ":3306").Status(); err == nil && status.Database != nil && status.Database.ReadWrite {
		logger.Info("database is up, skipping scale check")
	} else {
		scaled, err := scale.CheckScale(app, controllerKey, "mariadb", a.logger())
		if err != nil {
			httphelper.Error(w, err)
			return
		}

		// Cluster has yet to be scaled, return healthy
		if !scaled {
			w.WriteHeader(200)
			return
		}
	}

	db, err := a.connect()
	if err != nil {
		httphelper.Error(w, err)
		return
	}
	defer db.Close()

	if _, err := db.Exec("SELECT 1"); err != nil {
		httphelper.Error(w, err)
		return
	}
	w.WriteHeader(200)
}

func (a *API) connect() (*sql.DB, error) {
	dsn := &mariadb.DSN{
		Host:     serviceHost + ":3306",
		User:     "flynn",
		Password: os.Getenv("MYSQL_PWD"),
		Database: "mysql",
	}
	return sql.Open("mysql", dsn.String())
}

func (a *API) scaleUp() error {
	a.mtx.Lock()
	defer a.mtx.Unlock()

	// Ignore if already scaled up.
	if a.scaledUp {
		return nil
	}

	serviceAddr := serviceHost + ":3306"
	err := scale.ScaleUp(app, controllerKey, serviceAddr, "mariadb", singleton, a.logger())
	if err != nil {
		return err
	}

	// Mark as successfully scaled up.
	a.scaledUp = true
	return nil
}

func (a *API) logger() log15.Logger {
	return log15.New("app", "mariadb-web")
}
