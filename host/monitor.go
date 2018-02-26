package main

import (
	"encoding/json"
	"time"

	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/host/fixer"
	"github.com/flynn/flynn/pkg/cluster"
	"github.com/inconshreveable/log15"
)

const (
	checkInterval  = 10 * time.Second
	retryInterval  = 5 * time.Second
	deadlineLength = 60 * time.Second
)

var monitorLogger = log15.New("component", "cluster-monitor")

type MonitorMetadata struct {
	Enabled bool `json:"enabled,omitempty"`
	Hosts   int  `json:"hosts,omitempty"`
}

type Monitor struct {
	addr       string
	dm         *DiscoverdManager
	discoverd  *discoverdWrapper
	discClient *discoverd.Client
	monitorSvc discoverd.Service
	isLeader   bool
	c          *cluster.Client
	hostCount  int
	deadline   time.Time
	shutdownCh chan struct{}
	logger     log15.Logger
}

func NewMonitor(dm *DiscoverdManager, addr string, logger log15.Logger) *Monitor {
	return &Monitor{
		dm:         dm,
		discoverd:  nil,
		addr:       addr,
		shutdownCh: make(chan struct{}),
		logger:     logger,
	}
}

func (m *Monitor) waitDiscoverd() {
	for {
		if m.dm.localConnected() {
			m.discClient = discoverd.NewClient()
			break
		}
		time.Sleep(retryInterval)
	}
}

func (m *Monitor) waitRaftLeader() {
	for {
		_, err := m.discClient.RaftLeader()
		if err == nil {
			break
		}
		time.Sleep(retryInterval)
	}
}

func (m *Monitor) waitEnabled() {
	log := m.logger.New("fn", "waitEnabled")
	for {
		monitorMeta, err := m.monitorSvc.GetMeta()
		if err != nil {
			time.Sleep(retryInterval)
			continue
		}
		var decodedMeta MonitorMetadata
		if err := json.Unmarshal(monitorMeta.Data, &decodedMeta); err != nil {
			log.Error("monitor metadata unparsable")
			time.Sleep(retryInterval)
			continue
		}
		if decodedMeta.Enabled {
			m.hostCount = decodedMeta.Hosts
			break
		}
		time.Sleep(retryInterval)
	}

}

func (m *Monitor) waitRegister() {
	for {
		isLeader, err := m.discoverd.Register()
		if err == nil {
			m.isLeader = isLeader
			break
		}
		time.Sleep(retryInterval)
	}
}

func (m *Monitor) Run() {
	log := monitorLogger.New("fn", "Run")
	log.Info("waiting for discoverd")
	m.waitDiscoverd()

	log.Info("waiting for raft leader")
	m.waitRaftLeader()

	// we can connect the leader election wrapper now
	m.discoverd = newDiscoverdWrapper(m.addr+":1113", m.logger)
	// connect cluster client now that discoverd is up.
	m.c = cluster.NewClient()

	m.monitorSvc = discoverd.NewService("cluster-monitor")

	log.Info("waiting for monitor service to be enabled for this cluster")
	m.waitEnabled()

	log.Info("registering cluster-monitor")
	m.waitRegister()

	leaderCh := m.discoverd.LeaderCh()
	ticker := time.NewTicker(checkInterval)

	log.Info("starting monitor loop")
	for {
		var isLeader bool
		select {
		case <-m.shutdownCh:
			log.Info("shutting down monitor")
			return
		case isLeader = <-leaderCh:
			m.isLeader = isLeader
			continue
		default:
		}

		select {
		case <-ticker.C:
			if m.isLeader {
				m.checkCluster()
			}
		}
	}
}

func (m *Monitor) checkCluster() {
	log := monitorLogger.New("fn", "checkCluster")
	var faulted bool
	hosts, err := m.c.Hosts()
	if err != nil || len(hosts) < m.hostCount {
		log.Info("waiting for hosts", "current", len(hosts), "want", m.hostCount)
		return
	}

	controllerInstances, _ := discoverd.NewService("controller").Instances()
	if len(controllerInstances) == 0 {
		log.Error("did not find any controller api instances")
		faulted = true
	}

	if _, err := discoverd.NewService("controller-scheduler").Leader(); err != nil && !discoverd.IsNotFound(err) {
		log.Error("error getting scheduler leader, can't determine health")
	} else if err != nil {
		log.Error("scheduler is not up")
		faulted = true
	}

	if faulted && m.deadline.IsZero() {
		log.Error("cluster is unhealthy, setting fault")
		m.deadline = time.Now().Add(deadlineLength)
	} else if !faulted && !m.deadline.IsZero() {
		log.Info("cluster currently healthy, clearing fault")
		m.deadline = time.Time{}
	}

	if !m.deadline.IsZero() && time.Now().After(m.deadline) {
		log.Error("fault deadline reached")
		if err := m.repairCluster(); err != nil {
			log.Error("error repairing cluster", "err", err)
		}
		return
	}
}

func (m *Monitor) repairCluster() error {
	log := monitorLogger.New("fn", "repairCluster")
	log.Info("initiating cluster repair")
	hosts, err := m.c.Hosts()
	if err != nil {
		return err
	}
	f := fixer.NewClusterFixer(hosts, m.c, log)
	// killing the schedulers to prevent interference
	f.KillSchedulers()

	log.Info("checking status of sirenia databases")
	for _, db := range []string{"postgres", "mariadb", "mongodb"} {
		log.Info("checking for database state", "db", db)
		if _, err := discoverd.NewService(db).GetMeta(); err != nil {
			if discoverd.IsNotFound(err) {
				log.Info("skipping recovery of db, no state in discoverd", "db", db)
				continue
			}
			log.Error("error checking database state", "db", db)
			return err
		}
		if err := f.CheckSirenia(db); err != nil {
			if err := f.FixSirenia(db); err != nil {
				if db == "postgres" {
					return err
				} else {
					log.Error("failed database recovery", "db", db)
				}
			}
		}
	}

	// ensure controller api is working
	controllerService := discoverd.NewService("controller")
	controllerInstances, _ := controllerService.Instances()
	if len(controllerInstances) == 0 {
		controllerInstances, err = f.StartAppJob("controller", "web", "controller")
		if err != nil {
			return err
		}
	}
	// fix any formations and start the scheduler again
	if err := f.FixController(controllerInstances, true); err != nil {
		return err
	}
	// zero out the deadline timer
	m.deadline = time.Time{}
	return nil
}

func (m *Monitor) Shutdown() {
	close(m.shutdownCh)
}
