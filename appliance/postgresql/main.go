package main

import (
	"os"

	"github.com/flynn/flynn/Godeps/_workspace/src/gopkg.in/inconshreveable/log15.v2"
	"github.com/flynn/flynn/appliance/postgresql/state"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/shutdown"
)

func main() {
	serviceName := os.Getenv("FLYNN_POSTGRES")
	if serviceName == "" {
		serviceName = "postgres"
	}
	singleton := os.Getenv("SINGLETON") == "true"
	password := os.Getenv("PGPASSWORD")

	err := discoverd.DefaultClient.AddService(serviceName, &discoverd.ServiceConfig{
		LeaderType: discoverd.LeaderTypeManual,
	})
	if err != nil && !httphelper.IsObjectExistsError(err) {
		shutdown.Fatal(err)
	}
	inst := &discoverd.Instance{Addr: ":5432"}
	hb, err := discoverd.DefaultClient.RegisterInstance(serviceName, inst)
	if err != nil {
		shutdown.Fatal(err)
	}
	shutdown.BeforeExit(func() { hb.Close() })

	log := log15.New("app", "postgres")

	pg := NewPostgres(Config{
		ID:           inst.ID,
		Singleton:    singleton,
		BinDir:       "/usr/lib/postgresql/9.4/bin/",
		Password:     password,
		Logger:       log.New("component", "postgres"),
		ExtWhitelist: true,
		WaitUpstream: true,
		// TODO(titanous) investigate this:
		SHMType: "sysv", // the default on 9.4, 'posix' is not currently supported in our containers
	})
	dd := NewDiscoverd(discoverd.DefaultClient.Service(serviceName), log.New("component", "discoverd"))

	peer := state.NewPeer(inst, singleton, dd, pg, log.New("component", "peer"))
	shutdown.BeforeExit(func() { peer.Close() })

	go peer.Run()
	shutdown.Fatal(ServeHTTP(pg.(*Postgres), peer, hb, log.New("component", "http")))
	// TODO(titanous): clean shutdown of postgres
}
