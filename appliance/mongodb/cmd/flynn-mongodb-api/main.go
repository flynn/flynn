package main

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/random"
	"github.com/flynn/flynn/pkg/resource"
	"github.com/flynn/flynn/pkg/shutdown"
	sirenia "github.com/flynn/flynn/pkg/sirenia/client"
	"github.com/flynn/flynn/pkg/sirenia/scale"
	"github.com/julienschmidt/httprouter"
	"github.com/inconshreveable/log15"
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
)

var app = os.Getenv("FLYNN_APP_ID")
var controllerKey = os.Getenv("CONTROLLER_KEY")
var singleton = os.Getenv("SINGLETON")
var serviceName = os.Getenv("FLYNN_MONGO")
var serviceHost string

func init() {
	if serviceName == "" {
		serviceName = "mongodb"
	}
	serviceHost = fmt.Sprintf("leader.%s.discoverd", serviceName)
}

func main() {
	defer shutdown.Exit()

	api := &API{}

	router := httprouter.New()
	router.POST("/databases", api.createDatabase)
	router.DELETE("/databases", api.dropDatabase)
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

type API struct {
	mtx      sync.Mutex
	scaledUp bool
}

func (a *API) logger() log15.Logger {
	return log15.New("app", "mongodb-web")
}

func (a *API) createDatabase(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	// Ensure the cluster has been scaled up before attempting to create a database.
	if err := a.scaleUp(); err != nil {
		httphelper.Error(w, err)
		return
	}

	session, err := mgo.DialWithInfo(&mgo.DialInfo{
		Addrs:    []string{net.JoinHostPort(serviceHost, "27017")},
		Username: "flynn",
		Password: os.Getenv("MONGO_PWD"),
		Database: "admin",
	})
	if err != nil {
		httphelper.Error(w, err)
		return
	}
	defer session.Close()

	username, password, database := random.Hex(16), random.Hex(16), random.Hex(16)

	// Create a user
	if err := session.DB(database).Run(bson.D{
		{"createUser", username},
		{"pwd", password},
		{"roles", []bson.M{
			{"role": "dbOwner", "db": database},
		}},
	}, nil); err != nil {
		httphelper.Error(w, err)
		return
	}

	url := fmt.Sprintf("mongodb://%s:%s@%s:27017/%s", username, password, serviceHost, database)
	httphelper.JSON(w, 200, resource.Resource{
		ID: fmt.Sprintf("/databases/%s:%s", username, database),
		Env: map[string]string{
			"FLYNN_MONGO":    serviceName,
			"MONGO_HOST":     serviceHost,
			"MONGO_USER":     username,
			"MONGO_PWD":      password,
			"MONGO_DATABASE": database,
			"DATABASE_URL":   url,
		},
	})
}

func (a *API) dropDatabase(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	id := strings.SplitN(strings.TrimPrefix(req.FormValue("id"), "/databases/"), ":", 2)
	if len(id) != 2 || id[1] == "" {
		httphelper.ValidationError(w, "id", "is invalid")
		return
	}
	user, database := id[0], id[1]

	session, err := mgo.DialWithInfo(&mgo.DialInfo{
		Addrs:    []string{net.JoinHostPort(serviceHost, "27017")},
		Username: "flynn",
		Password: os.Getenv("MONGO_PWD"),
		Database: "admin",
	})
	if err != nil {
		httphelper.Error(w, err)
		return
	}
	defer session.Close()

	// Delete user.
	if err := session.DB(database).Run(bson.D{{"dropUser", user}}, nil); err != nil {
		httphelper.Error(w, err)
		return
	}

	// Delete database.
	if err := session.DB(database).Run(bson.D{{"dropDatabase", 1}}, nil); err != nil {
		httphelper.Error(w, err)
		return
	}

	w.WriteHeader(200)
}

func (a *API) ping(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	logger := a.logger().New("fn", "ping")

	logger.Info("checking status", "host", serviceHost)
	if status, err := sirenia.NewClient(serviceHost + ":27017").Status(); err == nil && status.Database != nil && status.Database.ReadWrite {
		logger.Info("database is up, skipping scale check")
	} else {
		scaled, err := scale.CheckScale(app, controllerKey, "mongodb", a.logger())
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

	session, err := mgo.DialWithInfo(&mgo.DialInfo{
		Addrs:    []string{net.JoinHostPort(serviceHost, "27017")},
		Username: "flynn",
		Password: os.Getenv("MONGO_PWD"),
		Database: "admin",
	})
	if err != nil {
		httphelper.Error(w, err)
		return
	}
	defer session.Close()

	w.WriteHeader(200)
}

func (a *API) scaleUp() error {
	a.mtx.Lock()
	defer a.mtx.Unlock()

	// Ignore if already scaled up.
	if a.scaledUp {
		return nil
	}

	serviceAddr := serviceHost + ":27017"
	err := scale.ScaleUp(app, controllerKey, serviceAddr, "mongodb", singleton, a.logger())
	if err != nil {
		return err
	}

	// Mark as successfully scaled up.
	a.scaledUp = true
	return nil
}
