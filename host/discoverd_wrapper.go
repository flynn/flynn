package main

import (
	"errors"
	"time"

	discoverd "github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/shutdown"
	"github.com/flynn/flynn/pkg/stream"
	"github.com/inconshreveable/log15"
)

const serviceName = "cluster-monitor"

func newDiscoverdWrapper(addr string, logger log15.Logger) *discoverdWrapper {
	return &discoverdWrapper{
		leader: make(chan bool),
		addr:   addr,
		logger: logger,
	}
}

type discoverdWrapper struct {
	addr   string
	leader chan bool
	logger log15.Logger
}

func (d *discoverdWrapper) Register() (bool, error) {
	log := d.logger.New("fn", "discoverd.Register")

	log.Info("registering with service discovery")
	hb, err := discoverd.AddServiceAndRegister(serviceName, d.addr)
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
				if leader == nil {
					// a nil leader indicates there are no instances for
					// the service, ignore and wait for an actual leader
					log.Warn("received nil leader event")
					continue
				}
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

// Only one receiver can consume from this channel at a time.
func (d *discoverdWrapper) LeaderCh() chan bool {
	return d.leader
}
