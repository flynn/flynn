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
	"github.com/flynn/flynn/host/volume"
	"github.com/flynn/flynn/pkg/cluster"
	"github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/shutdown"
	sd "github.com/flynn/flynn/pkg/sirenia/discoverd"
	"github.com/flynn/flynn/pkg/sirenia/state"
	"gopkg.in/inconshreveable/log15.v2"
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
	volID := os.Getenv("VOLUME_0")
	if volID == "" {
		shutdown.Fatalf("error getting primary volume ID, VOLUME_0 not set")
	}

	hostID, _ := cluster.ExtractHostID(os.Getenv("FLYNN_JOB_ID"))
	host, err := cluster.NewClient().Host(hostID)
	if err != nil {
		shutdown.Fatal(err)
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
		Meta: map[string]string{mongoIdKey: volID},
	}
	hb, err := discoverd.DefaultClient.RegisterInstance(serviceName, inst)
	if err != nil {
		shutdown.Fatal(err)
	}
	shutdown.BeforeExit(func() { hb.Close() })

	log := log15.New("app", "mongodb")

	process := mongodb.NewProcess()
	process.ID = volID
	process.Password = password
	process.Singleton = singleton
	process.ServerID = serverId
	process.Host = ip

	dd := sd.NewDiscoverd(discoverd.DefaultClient.Service(serviceName), log.New("component", "discoverd"))

	peer := state.NewPeer(inst, volID, mongoIdKey, singleton, dd, process, log.New("component", "peer"))
	shutdown.BeforeExit(func() { peer.Close() })

	go peer.Run()

	handler := mongodb.NewHandler()
	handler.Process = process
	handler.Peer = peer
	handler.Heartbeater = hb
	handler.Logger = log.New("component", "http")
	handler.Snapshot = func() (*volume.Info, error) { return host.CreateSnapshot(volID) }

	shutdown.Fatal(http.ListenAndServe(":"+httpPort, handler))
}

func ipToId(ip net.IP) uint32 {
	ip = ip.To4()
	return binary.BigEndian.Uint32([]byte(ip))
}
