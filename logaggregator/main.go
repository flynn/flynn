package main

import (
	"flag"
	"net"
	"os"

	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/logaggregator/client"
	"github.com/flynn/flynn/pkg/shutdown"

	"github.com/flynn/flynn/Godeps/_workspace/src/gopkg.in/inconshreveable/log15.v2"
)

func main() {
	defer shutdown.Exit()

	apiPort := os.Getenv("PORT")
	if apiPort == "" {
		apiPort = "5000"
	}

	logAddr := flag.String("logaddr", ":3000", "syslog input listen address")
	apiAddr := flag.String("apiaddr", ":"+apiPort, "api listen address")
	flag.Parse()

	conf := ServerConfig{
		SyslogAddr:  *logAddr,
		ApiAddr:     *apiAddr,
		Discoverd:   discoverd.DefaultClient,
		ServiceName: "logaggregator",
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
