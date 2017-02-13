package main

import (
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/provider"
	"github.com/flynn/flynn/pkg/random"
	"github.com/flynn/flynn/pkg/shutdown"
	sp "github.com/flynn/flynn/pkg/sirenia/provider"
	"github.com/flynn/flynn/pkg/status/protobuf"
	"google.golang.org/grpc"
	"gopkg.in/inconshreveable/log15.v2"
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
)

var appID = os.Getenv("FLYNN_APP_ID")
var controllerKey = os.Getenv("CONTROLLER_KEY")
var serviceName = os.Getenv("FLYNN_MONGO")
var singleton bool
var enableScaling bool
var serviceHost string
var serviceAddr string

func init() {
	if serviceName == "" {
		serviceName = "mongodb"
	}
	serviceHost = fmt.Sprintf("leader.%s.discoverd", serviceName)
	serviceAddr = serviceHost + ":27017"
	if os.Getenv("SINGLETON") == "true" {
		singleton = true
	}
	if os.Getenv("ENABLE_SCALING") == "true" {
		enableScaling = true
	}
}

func main() {
	defer shutdown.Exit()

	s := grpc.NewServer()
	rpcServer := sp.NewProvider(&DB{}, appID, controllerKey, serviceHost, serviceAddr, serviceName, singleton, enableScaling)
	provider.RegisterProviderServer(s, rpcServer)
	status.RegisterStatusServer(s, rpcServer)

	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}
	addr := ":" + port

	l, err := net.Listen("tcp", addr)
	if err != nil {
		shutdown.Fatal(err)
	}

	hb, err := discoverd.AddServiceAndRegister(serviceName+"-api", addr)
	if err != nil {
		shutdown.Fatal(err)
	}
	shutdown.BeforeExit(func() { hb.Close() })

	shutdown.Fatal(s.Serve(l))
}

type DB struct{}

func (d *DB) Logger() log15.Logger {
	return log15.New("app", "mongodb-web")
}

func (d *DB) Provision() (string, map[string]string, error) {
	// Ensure the cluster has been scaled up before attempting to create a database.
	session, err := mgo.DialWithInfo(&mgo.DialInfo{
		Addrs:    []string{serviceAddr},
		Username: "flynn",
		Password: os.Getenv("MONGO_PWD"),
		Database: "admin",
	})
	if err != nil {
		return "", nil, err
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
		return "", nil, err
	}

	url := fmt.Sprintf("mongodb://%s:%s@%s/%s", username, password, serviceAddr, database)
	id := fmt.Sprintf("/databases/%s:%s", username, database)
	env := map[string]string{
		"FLYNN_MONGO":    serviceName,
		"MONGO_HOST":     serviceHost,
		"MONGO_USER":     username,
		"MONGO_PWD":      password,
		"MONGO_DATABASE": database,
		"DATABASE_URL":   url,
	}
	return id, env, nil
}

func (d *DB) Deprovision(reqId string) error {
	id := strings.SplitN(strings.TrimPrefix(reqId, "/databases/"), ":", 2)
	if len(id) != 2 || id[1] == "" {
		return fmt.Errorf("id is invalid")
	}
	user, database := id[0], id[1]

	session, err := mgo.DialWithInfo(&mgo.DialInfo{
		Addrs:    []string{serviceAddr},
		Username: "flynn",
		Password: os.Getenv("MONGO_PWD"),
		Database: "admin",
	})
	if err != nil {
		return err
	}
	defer session.Close()

	// Delete user.
	if err := session.DB(database).Run(bson.D{{"dropUser", user}}, nil); err != nil {
		return err
	}

	// Delete database.
	if err := session.DB(database).Run(bson.D{{"dropDatabase", 1}}, nil); err != nil {
		return err
	}

	return nil
}

func (d *DB) Ping() error {
	session, err := mgo.DialWithInfo(&mgo.DialInfo{
		Addrs:    []string{serviceAddr},
		Username: "flynn",
		Password: os.Getenv("MONGO_PWD"),
		Database: "admin",
	})
	if err != nil {
		return err
	}
	defer session.Close()

	if err := session.DB("admin").Run(bson.D{{"ping", 1}}, nil); err != nil {
		return err
	}
	return nil
}
