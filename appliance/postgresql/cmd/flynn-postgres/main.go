package main

import (
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"

	"github.com/flynn/flynn/appliance/postgresql"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/random"
	"github.com/flynn/flynn/pkg/shutdown"
	sd "github.com/flynn/flynn/pkg/sirenia/discoverd"
	"github.com/flynn/flynn/pkg/sirenia/state"
	"gopkg.in/inconshreveable/log15.v2"
)

func main() {
	serviceName := os.Getenv("FLYNN_POSTGRES")
	if serviceName == "" {
		serviceName = "postgres"
	}
	singleton := os.Getenv("SINGLETON") == "true"
	password := os.Getenv("PGPASSWORD")
	httpPort := os.Getenv("HTTP_PORT")
	if httpPort == "" {
		httpPort = "5433"
	}

	const dataDir = "/data"
	idFile := filepath.Join(dataDir, "instance_id")
	idBytes, err := ioutil.ReadFile(idFile)
	if err != nil && !os.IsNotExist(err) {
		shutdown.Fatalf("error reading instance ID: %s", err)
	}
	id := string(idBytes)
	if len(id) == 0 {
		id = random.UUID()
		if err := ioutil.WriteFile(idFile, []byte(id), 0644); err != nil {
			shutdown.Fatalf("error writing instance ID: %s", err)
		}
	}

	err = discoverd.DefaultClient.AddService(serviceName, &discoverd.ServiceConfig{
		LeaderType: discoverd.LeaderTypeManual,
	})
	if err != nil && !httphelper.IsObjectExistsError(err) {
		shutdown.Fatal(err)
	}
	inst := &discoverd.Instance{
		Addr: ":5432",
		Meta: map[string]string{postgresql.IDKey: id},
	}
	hb, err := discoverd.DefaultClient.RegisterInstance(serviceName, inst)
	if err != nil {
		shutdown.Fatal(err)
	}
	shutdown.BeforeExit(func() { hb.Close() })

	log := log15.New("app", "postgres")

	process := postgresql.NewProcess(postgresql.Config{
		ID:           id,
		Singleton:    singleton,
		DataDir:      filepath.Join(dataDir, "db"),
		BinDir:       "/usr/lib/postgresql/9.5/bin/",
		Password:     password,
		Logger:       log.New("component", "postgres"),
		ExtWhitelist: true,
		WaitUpstream: true,
	})
	dd := sd.NewDiscoverd(discoverd.DefaultClient.Service(serviceName), log.New("component", "discoverd"))

	peer := state.NewPeer(inst, id, postgresql.IDKey, singleton, dd, process, log.New("component", "peer"))
	shutdown.BeforeExit(func() { peer.Close() })

	go peer.Run()

	handler := postgresql.NewHandler()
	handler.Process = process
	handler.Peer = peer
	handler.Heartbeater = hb
	handler.Logger = log.New("component", "http")

	shutdown.Fatal(http.ListenAndServe(":"+httpPort, handler))
	// TODO(titanous): clean shutdown of postgres
}
