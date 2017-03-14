package main

import (
	"encoding/binary"
	"net"
	"net/http"
	"os"

	"github.com/flynn/flynn/appliance/mariadb"
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
	mariaIdKey = "MARIADB_ID"
)

func main() {
	serviceName := os.Getenv("FLYNN_MYSQL")
	if serviceName == "" {
		serviceName = "mariadb"
	}
	singleton := os.Getenv("SINGLETON") == "true"
	password := os.Getenv("MYSQL_PWD")
	httpPort := os.Getenv("HTTP_PORT")
	ip := os.Getenv("EXTERNAL_IP")
	if httpPort == "" {
		httpPort = "3307"
	}
	serverId := ip2id(net.ParseIP(ip))

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

	err = discoverd.DefaultClient.AddService(serviceName, &discoverd.ServiceConfig{
		LeaderType: discoverd.LeaderTypeManual,
	})
	if err != nil && !httphelper.IsObjectExistsError(err) {
		shutdown.Fatal(err)
	}
	inst := &discoverd.Instance{
		Addr: ":" + mariadb.DefaultPort,
		Meta: map[string]string{mariaIdKey: volID},
	}
	hb, err := discoverd.DefaultClient.RegisterInstance(serviceName, inst)
	if err != nil {
		shutdown.Fatal(err)
	}
	shutdown.BeforeExit(func() { hb.Close() })

	log := log15.New("app", "mariadb")

	process := mariadb.NewProcess()
	process.Password = password
	process.Singleton = singleton
	process.ServerID = serverId

	dd := sd.NewDiscoverd(discoverd.DefaultClient.Service(serviceName), log.New("component", "discoverd"))

	peer := state.NewPeer(inst, volID, mariaIdKey, singleton, dd, process, log.New("component", "peer"))
	shutdown.BeforeExit(func() { peer.Close() })

	go peer.Run()

	handler := mariadb.NewHandler()
	handler.Process = process
	handler.Peer = peer
	handler.Heartbeater = hb
	handler.Logger = log.New("component", "http")
	handler.Snapshot = func() (*volume.Info, error) { return host.CreateSnapshot(volID) }

	shutdown.Fatal(http.ListenAndServe(":"+httpPort, handler))
}

func ip2id(ip net.IP) uint32 {
	ip = ip.To4()
	return binary.BigEndian.Uint32([]byte(ip))
}
