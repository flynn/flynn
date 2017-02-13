package main

import (
	"database/sql"
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/flynn/flynn/appliance/mariadb"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/provider"
	"github.com/flynn/flynn/pkg/random"
	"github.com/flynn/flynn/pkg/shutdown"
	sp "github.com/flynn/flynn/pkg/sirenia/provider"
	"github.com/flynn/flynn/pkg/status/protobuf"
	_ "github.com/go-sql-driver/mysql"
	"google.golang.org/grpc"
	"gopkg.in/inconshreveable/log15.v2"
)

var serviceName = os.Getenv("FLYNN_MYSQL")
var appID = os.Getenv("FLYNN_APP_ID")
var controllerKey = os.Getenv("CONTROLLER_KEY")
var singleton bool
var enableScaling bool
var serviceHost string
var serviceAddr string

func init() {
	if serviceName == "" {
		serviceName = "mariadb"
	}
	serviceHost = fmt.Sprintf("leader.%s.discoverd", serviceName)
	serviceAddr = serviceHost + ":3306"
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

func (d *DB) Provision() (string, map[string]string, error) {
	db, err := d.connect()
	if err != nil {
		return "", nil, err
	}
	defer db.Close()

	username, password, database := random.Hex(16), random.Hex(16), random.Hex(16)
	if _, err := db.Exec(fmt.Sprintf("CREATE USER '%s'@'%%' IDENTIFIED BY '%s'", username, password)); err != nil {
		return "", nil, err
	}
	if _, err := db.Exec(fmt.Sprintf("CREATE DATABASE `%s`", database)); err != nil {
		db.Exec(fmt.Sprintf("DROP USER '%s'", username))
		return "", nil, err
	}
	if _, err := db.Exec(fmt.Sprintf("GRANT ALL ON `%s`.* TO '%s'@'%%'", database, username)); err != nil {
		db.Exec(fmt.Sprintf("DROP DATABASE `%s`", database))
		db.Exec(fmt.Sprintf("DROP USER '%s'", username))
		return "", nil, err
	}

	url := fmt.Sprintf("mysql://%s:%s@%s/%s", username, password, serviceAddr, database)
	id := fmt.Sprintf("/databases/%s:%s", username, database)
	env := map[string]string{
		"FLYNN_MYSQL":    serviceName,
		"MYSQL_HOST":     serviceHost,
		"MYSQL_USER":     username,
		"MYSQL_PWD":      password,
		"MYSQL_DATABASE": database,
		"DATABASE_URL":   url,
	}
	return id, env, nil
}

func (d *DB) Deprovision(reqId string) error {
	id := strings.SplitN(strings.TrimPrefix(reqId, "/databases/"), ":", 2)
	if len(id) != 2 || id[1] == "" {
		return fmt.Errorf("id is invalid")
	}

	db, err := d.connect()
	if err != nil {
		return err
	}
	defer db.Close()

	if _, err := db.Exec(fmt.Sprintf("DROP DATABASE `%s`", id[1])); err != nil {
		return err
	}

	if _, err := db.Exec(fmt.Sprintf("DROP USER '%s'", id[0])); err != nil {
		return err
	}
	return nil

}

func (d *DB) Ping() error {
	db, err := d.connect()
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err := db.Exec("SELECT 1"); err != nil {
		return err
	}
	return nil
}

func (d *DB) Logger() log15.Logger {
	return log15.New("app", "mariadb-web")
}

func (d *DB) connect() (*sql.DB, error) {
	dsn := &mariadb.DSN{
		Host:     serviceAddr,
		User:     "flynn",
		Password: os.Getenv("MYSQL_PWD"),
		Database: "mysql",
	}
	return sql.Open("mysql", dsn.String())
}
