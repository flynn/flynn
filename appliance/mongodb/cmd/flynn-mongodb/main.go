package main

import (
	"encoding/binary"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"path/filepath"

	"github.com/flynn/flynn/appliance/mongodb"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/random"
	"github.com/flynn/flynn/pkg/shutdown"
	sd "github.com/flynn/flynn/pkg/sirenia/discoverd"
	"github.com/flynn/flynn/pkg/sirenia/state"
	"github.com/inconshreveable/log15"
)

const (
	mongoIdKey = "MONGODB_ID"
)

func main() {
	serviceName := os.Getenv("FLYNN_MONGO")
	if serviceName == "" {
		serviceName = "mongodb"
	}
	singleton := os.Getenv("SINGLETON") == "true"
	password := os.Getenv("MONGO_PWD")
	httpPort := os.Getenv("HTTP_PORT")
	ip := os.Getenv("EXTERNAL_IP")
	if httpPort == "" {
		httpPort = "27018"
	}
	serverId := ipToId(net.ParseIP(ip))

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

	keyFile := filepath.Join(dataDir, "Keyfile")
	if err := ioutil.WriteFile(keyFile, []byte(password), 0600); err != nil {
		shutdown.Fatalf("error writing keyfile: %s", err)
	}

	err = discoverd.DefaultClient.AddService(serviceName, &discoverd.ServiceConfig{
		LeaderType: discoverd.LeaderTypeManual,
	})
	if err != nil && !httphelper.IsObjectExistsError(err) {
		shutdown.Fatal(err)
	}
	inst := &discoverd.Instance{
		Addr: ":" + mongodb.DefaultPort,
		Meta: map[string]string{mongoIdKey: id},
	}
	hb, err := discoverd.DefaultClient.RegisterInstance(serviceName, inst)
	if err != nil {
		shutdown.Fatal(err)
	}
	shutdown.BeforeExit(func() { hb.Close() })

	log := log15.New("app", "mongodb")

	process := mongodb.NewProcess()
	process.Password = password
	process.Singleton = singleton
	process.ServerID = serverId
	process.Host = ip

	dd := sd.NewDiscoverd(discoverd.DefaultClient.Service(serviceName), log.New("component", "discoverd"))

	peer := state.NewPeer(inst, id, mongoIdKey, singleton, dd, process, log.New("component", "peer"))
	shutdown.BeforeExit(func() { peer.Close() })

	go peer.Run()

	handler := mongodb.NewHandler()
	handler.Process = process
	handler.Peer = peer
	handler.Heartbeater = hb
	handler.Logger = log.New("component", "http")

	shutdown.Fatal(http.ListenAndServe(":"+httpPort, handler))
}

func ipToId(ip net.IP) uint32 {
	ip = ip.To4()
	return binary.BigEndian.Uint32([]byte(ip))
}
