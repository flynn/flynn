package main

import (
	"net"
	"os"

	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/logaggregator/client"
	"github.com/flynn/flynn/pkg/shutdown"

	"gopkg.in/inconshreveable/log15.v2"
)

func main() {
	defer shutdown.Exit()

	apiPort := os.Getenv("PORT_0")
	if apiPort == "" {
		apiPort = "5000"
	}

	logPort := os.Getenv("PORT_1")
	if logPort == "" {
		logPort = "3000"
	}

	serviceName := os.Getenv("SERVICE_NAME")
	if serviceName == "" {
		serviceName = "logaggregator"
	}

	conf := ServerConfig{
		SyslogAddr:  ":" + logPort,
		ApiAddr:     ":" + apiPort,
		Discoverd:   discoverd.DefaultClient,
		ServiceName: serviceName,
	}

	srv := NewServer(conf)
	shutdown.BeforeExit(srv.Shutdown)

	// get leader for snapshot (if any)
	leader, err := conf.Discoverd.Service(conf.ServiceName).Leader()
	if err == nil {
		host, _, _ := net.SplitHostPort(leader.Addr)
		log15.Info("loading snapshot from leader", "leader", host)

		c, _ := client.New("http://" + host)
		snapshot, err := c.GetSnapshot()
		if err == nil {
			if err := srv.LoadSnapshot(snapshot); err != nil {
				log15.Error("error receiving snapshot from leader", "error", err)
			}
			snapshot.Close()
		} else {
			log15.Error("error getting snapshot from leader", "error", err)
		}
	} else {
		log15.Info("error finding leader for snapshot", "error", err)
	}

	if err := srv.Start(); err != nil {
		shutdown.Fatal(err)
	}
	<-make(chan struct{})
}
