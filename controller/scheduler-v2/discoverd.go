package main

import (
	"errors"
	"os"
	"time"

	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/shutdown"
	"github.com/flynn/flynn/pkg/stream"
)

const serviceName = "controller-scheduler"

type Discoverd interface {
	Register() (bool, error)
	LeaderCh() chan bool
}

func newDiscoverdWrapper() *discoverdWrapper {
	return &discoverdWrapper{leader: make(chan bool)}
}

type discoverdWrapper struct {
	leader chan bool
}

func (d *discoverdWrapper) Register() (bool, error) {
	log := logger.New("fn", "discoverd.Register")

	log.Info("registering with service discovery")
	hb, err := discoverd.AddServiceAndRegister(serviceName, ":"+os.Getenv("PORT"))
	if err != nil {
		log.Error("error registering with service discovery", "err", err)
		return false, err
	}
	shutdown.BeforeExit(func() { hb.Close() })

	selfAddr := hb.Addr()
	log = log.New("self.addr", selfAddr)

	service := discoverd.NewService(serviceName)
	var leaders chan *discoverd.Instance
	var stream stream.Stream
	connect := func() (err error) {
		log.Info("connecting service leader stream")
		leaders = make(chan *discoverd.Instance)
		stream, err = service.Leaders(leaders)
		if err != nil {
			log.Error("error connecting service leader stream", "err", err)
		}
		return
	}
	if err := connect(); err != nil {
		return false, err
	}

	go func() {
	outer:
		for {
			for leader := range leaders {
				log.Info("received leader event", "leader.addr", leader.Addr)
				d.leader <- leader.Addr == selfAddr
			}
			log.Warn("service leader stream disconnected", "err", stream.Err())
			for {
				if err := connect(); err == nil {
					continue outer
				}
				time.Sleep(100 * time.Millisecond)
			}
		}
	}()

	select {
	case isLeader := <-d.leader:
		return isLeader, nil
	case <-time.After(30 * time.Second):
		return false, errors.New("timed out waiting for current service leader")
	}
}

func (d *discoverdWrapper) LeaderCh() chan bool {
	return d.leader
}
