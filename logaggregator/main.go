package main

import (
	"flag"
	"os"

	"github.com/flynn/flynn/discoverd/client"
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
	snapshotPath := flag.String("snapshot", "", "snapshot path")
	flag.Parse()

	conf := ServerConfig{
		SyslogAddr:  *logAddr,
		ApiAddr:     *apiAddr,
		Discoverd:   discoverd.DefaultClient,
		ServiceName: "logaggregator",
	}

	srv, err := NewServer(conf)
	if err != nil {
		shutdown.Fatal(err)
	}
	shutdown.BeforeExit(srv.Shutdown)

	if *snapshotPath != "" {
		if err := srv.LoadSnapshot(*snapshotPath); err != nil {
			shutdown.Fatal(err)
		}
		shutdown.BeforeExit(func() {
			if err := srv.WriteSnapshot(*snapshotPath); err != nil {
				log15.Error("snapshot error", "err", err)
			}
		})
	}

	shutdown.Fatal(srv.Run())
}
