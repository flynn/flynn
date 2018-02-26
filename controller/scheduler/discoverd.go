package main

import (
	"os"
	"time"

	discoverd "github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/shutdown"
	"github.com/flynn/flynn/pkg/stream"
	"github.com/inconshreveable/log15"
)

const serviceName = "controller-scheduler"

type Discoverd interface {
	Register() bool
	LeaderCh() chan bool
}

func newDiscoverdWrapper(l log15.Logger) *discoverdWrapper {
	return &discoverdWrapper{
		leader: make(chan bool),
		logger: l,
	}
}

type discoverdWrapper struct {
	leader chan bool
	logger log15.Logger
}

func (d *discoverdWrapper) Register() bool {
	log := d.logger.New("fn", "discoverd.Register")

	var hb discoverd.Heartbeater
	for {
		var err error
		log.Info("registering with service discovery")
		hb, err = discoverd.AddServiceAndRegister(serviceName, ":"+os.Getenv("PORT"))
		if err == nil {
			break
		}
		log.Error("error registering with service discovery", "err", err)
		time.Sleep(time.Second)
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

	go func() {
		for {
			for {
				if err := connect(); err == nil {
					break
				}
				time.Sleep(100 * time.Millisecond)
			}
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
		}
	}()

	start := time.Now()
	tick := time.Tick(30 * time.Second)
	for {
		select {
		case isLeader := <-d.leader:
			return isLeader
		case <-tick:
			log.Warn("still waiting for current service leader", "duration", time.Since(start))
		}
	}
}

func (d *discoverdWrapper) LeaderCh() chan bool {
	return d.leader
}
