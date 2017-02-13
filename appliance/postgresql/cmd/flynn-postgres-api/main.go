package main

import (
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/postgres"
	"github.com/flynn/flynn/pkg/provider"
	"github.com/flynn/flynn/pkg/random"
	"github.com/flynn/flynn/pkg/shutdown"
	sp "github.com/flynn/flynn/pkg/sirenia/provider"
	"github.com/flynn/flynn/pkg/status/protobuf"
	"google.golang.org/grpc"
	"gopkg.in/inconshreveable/log15.v2"
)

const (
	disallowConns   = `UPDATE pg_database SET datallowconn = FALSE WHERE datname = $1`
	disconnectConns = `
SELECT pg_terminate_backend(pg_stat_activity.pid)
FROM pg_stat_activity
WHERE pg_stat_activity.datname = $1
  AND pid <> pg_backend_pid();`
)

var appID = os.Getenv("FLYNN_APP_ID")
var controllerKey = os.Getenv("CONTROLLER_KEY")
var serviceName = os.Getenv("FLYNN_POSTGRES")
var singleton bool
var enableScaling bool
var serviceHost string
var serviceAddr string

func init() {
	if serviceName == "" {
		serviceName = "postgres"
	}
	serviceHost = fmt.Sprintf("leader.%s.discoverd", serviceName)
	serviceAddr = serviceHost + ":5432"
	if os.Getenv("SINGLETON") == "true" {
		singleton = true
	}
	if os.Getenv("ENABLE_SCALING") == "true" {
		enableScaling = true
	}
}

func main() {
	defer shutdown.Exit()

	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}
	addr := ":" + port

	db := postgres.Wait(&postgres.Conf{
		Service:  serviceName,
		User:     "flynn",
		Password: os.Getenv("PGPASSWORD"),
		Database: "postgres",
	}, nil)

	s := grpc.NewServer()
	rpcServer := sp.NewProvider(&DB{db}, appID, controllerKey, serviceHost, serviceAddr, serviceName, singleton, enableScaling)
	provider.RegisterProviderServer(s, rpcServer)
	status.RegisterStatusServer(s, rpcServer)

	l, err := net.Listen("tcp", ":"+port)
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

type DB struct {
	db *postgres.DB
}

func (d *DB) Provision() (string, map[string]string, error) {
	username, password, database := random.Hex(16), random.Hex(16), random.Hex(16)

	if err := d.db.Exec(fmt.Sprintf(`CREATE USER "%s" WITH PASSWORD '%s'`, username, password)); err != nil {
		return "", nil, err
	}
	if err := d.db.Exec(fmt.Sprintf(`CREATE DATABASE "%s"`, database)); err != nil {
		d.db.Exec(fmt.Sprintf(`DROP USER "%s"`, username))
		return "", nil, err
	}
	if err := d.db.Exec(fmt.Sprintf(`GRANT ALL ON DATABASE "%s" TO "%s"`, database, username)); err != nil {
		d.db.Exec(fmt.Sprintf(`DROP DATABASE "%s"`, database))
		d.db.Exec(fmt.Sprintf(`DROP USER "%s"`, username))
		return "", nil, err
	}

	url := fmt.Sprintf("postgres://%s:%s@%s/%s", username, password, serviceAddr, database)
	id := fmt.Sprintf("/databases/%s:%s", username, database)
	env := map[string]string{
		"FLYNN_POSTGRES": serviceName,
		"PGHOST":         serviceHost,
		"PGUSER":         username,
		"PGPASSWORD":     password,
		"PGDATABASE":     database,
		"DATABASE_URL":   url,
	}
	return id, env, nil
}

func (d *DB) Deprovision(reqId string) error {
	id := strings.SplitN(strings.TrimPrefix(reqId, "/databases/"), ":", 2)
	if len(id) != 2 || id[1] == "" {
		return fmt.Errorf("id is invalid")
	}

	// disable new connections to the target database
	if err := d.db.Exec(disallowConns, id[1]); err != nil {
		return err
	}

	// terminate current connections
	if err := d.db.Exec(disconnectConns, id[1]); err != nil {
		return err
	}

	if err := d.db.Exec(fmt.Sprintf(`DROP DATABASE "%s"`, id[1])); err != nil {
		return err
	}

	if err := d.db.Exec(fmt.Sprintf(`DROP USER "%s"`, id[0])); err != nil {
		return err
	}
	return nil
}

func (d *DB) Ping() error {
	if err := d.db.Exec("SELECT 1"); err != nil {
		return err
	}
	return nil
}

func (d *DB) Logger() log15.Logger {
	return log15.New("app", "postgres-web")
}
