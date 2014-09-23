package main

import (
	"log"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/technoweenie/grohl"
	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/controller/utils"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/pkg/attempt"
	"github.com/flynn/flynn/pkg/cluster"
)

var backoffPeriod = 10 * time.Minute

// Allow mocking time.AfterFunc in tests
var timeAfterFunc = time.AfterFunc

func main() {
	grohl.AddContext("app", "controller-scheduler")
	grohl.Log(grohl.Data{"at": "start"})

	cc, err := controller.NewClient("", os.Getenv("AUTH_KEY"))
	if err != nil {
		log.Fatal(err)
	}
	cl, err := cluster.NewClient()
	if err != nil {
		log.Fatal(err)
	}
	c := newContext(cc, cl)

	grohl.Log(grohl.Data{"at": "leaderwait"})
	leaderWait, err := discoverd.RegisterAndStandby("flynn-controller-scheduler", ":"+os.Getenv("PORT"), nil)
	if err != nil {
		log.Fatal(err)
	}
	<-leaderWait
	grohl.Log(grohl.Data{"at": "leader"})

	// TODO: periodic full cluster sync for anti-entropy
	c.watchFormations(nil, nil)
}

func newContext(cc controllerClient, cl clusterClient) *context {
	return &context{
		controllerClient: cc,
		clusterClient:    cl,
		formations:       NewFormations(),
		hosts:            newHostClients(),
		jobs:             newJobMap(),
		omni:             make(map[*Formation]struct{}),
	}
}

type context struct {
	controllerClient
	clusterClient
	formations *Formations
	omni       map[*Formation]struct{}
	omniMtx    sync.RWMutex

	hosts *hostClients
	jobs  *jobMap
	mtx   sync.RWMutex
}

type clusterClient interface {
	ListHosts() (map[string]host.Host, error)
	AddJobs(req *host.AddJobsReq) (*host.AddJobsRes, error)
	DialHost(id string) (cluster.Host, error)
	StreamHostEvents(ch chan<- *host.HostEvent) cluster.Stream
}

type controllerClient interface {
	GetRelease(releaseID string) (*ct.Release, error)
	GetArtifact(artifactID string) (*ct.Artifact, error)
	GetFormation(appID, releaseID string) (*ct.Formation, error)
	StreamFormations(since *time.Time) (*controller.FormationUpdates, *error)
	PutJob(job *ct.Job) error
}

func (c *context) syncCluster(events chan<- *host.Event) {
	g := grohl.NewContext(grohl.Data{"fn": "syncCluster"})

	artifacts := make(map[string]*ct.Artifact)
	releases := make(map[string]*ct.Release)
	rectify := make(map[*Formation]struct{})

	go c.watchHosts(events)

	hosts, err := c.ListHosts()
	if err != nil {
		// TODO: log/handle error
	}

	c.mtx.Lock()
	for _, h := range hosts {
		for _, job := range h.Jobs {
			appID := job.Metadata["flynn-controller.app"]
			appName := job.Metadata["flynn-controller.app_name"]
			releaseID := job.Metadata["flynn-controller.release"]
			jobType := job.Metadata["flynn-controller.type"]
			gg := g.New(grohl.Data{"host.id": h.ID, "job.id": job.ID, "app.id": appID, "release.id": releaseID, "type": jobType})

			if appID == "" || releaseID == "" {
				continue
			}
			if job := c.jobs.Get(h.ID, job.ID); job != nil {
				continue
			}

			f := c.formations.Get(appID, releaseID)
			if f == nil {
				release := releases[releaseID]
				if release == nil {
					release, err = c.GetRelease(releaseID)
					if err != nil {
						gg.Log(grohl.Data{"at": "getRelease", "status": "error", "err": err})
						continue
					}
					releases[release.ID] = release
				}

				artifact := artifacts[release.ArtifactID]
				if artifact == nil {
					artifact, err = c.GetArtifact(release.ArtifactID)
					if err != nil {
						gg.Log(grohl.Data{"at": "getArtifact", "status": "error", "err": err})
						continue
					}
					artifacts[artifact.ID] = artifact
				}

				formation, err := c.GetFormation(appID, releaseID)
				if err != nil {
					gg.Log(grohl.Data{"at": "getFormation", "status": "error", "err": err})
					continue
				}

				f = NewFormation(c, &ct.ExpandedFormation{
					App:       &ct.App{ID: appID, Name: appName},
					Release:   release,
					Artifact:  artifact,
					Processes: formation.Processes,
				})
				gg.Log(grohl.Data{"at": "addFormation"})
				f = c.formations.Add(f)
			}

			gg.Log(grohl.Data{"at": "addJob"})
			go c.PutJob(&ct.Job{
				ID:        h.ID + "-" + job.ID,
				AppID:     appID,
				ReleaseID: releaseID,
				Type:      jobType,
				State:     "up",
			})
			j := f.jobs.Add(jobType, h.ID, job.ID)
			j.Formation = f
			c.jobs.Add(j)
			rectify[f] = struct{}{}
		}
	}
	c.mtx.Unlock()

	for f := range rectify {
		go f.Rectify()
	}
}

func (c *context) watchFormations(events chan<- *FormationEvent, hostEvents chan<- *host.Event) {
	g := grohl.NewContext(grohl.Data{"fn": "watchFormations"})

	c.syncCluster(hostEvents)
	if events != nil {
		events <- &FormationEvent{}
	}

	var attempts int
	var lastUpdatedAt time.Time
	for {
		// wait a second if we've tried more than once
		attempts++
		if attempts > 1 {
			time.Sleep(time.Second)
		}

		g.Log(grohl.Data{"at": "connect", "attempt": attempts})
		updates, err := c.StreamFormations(&lastUpdatedAt)
		for ef := range updates.Chan {
			// we are now connected so reset attempts
			attempts = 0

			if ef.App == nil {
				// sentinel
				continue
			}
			lastUpdatedAt = ef.UpdatedAt
			f := c.formations.Get(ef.App.ID, ef.Release.ID)
			if f != nil {
				g.Log(grohl.Data{"app.id": ef.App.ID, "release.id": ef.Release.ID, "at": "update"})
				f.SetProcesses(ef.Processes)
			} else {
				g.Log(grohl.Data{"app.id": ef.App.ID, "release.id": ef.Release.ID, "at": "new"})
				f = NewFormation(c, ef)
				c.formations.Add(f)
			}
			// check for omnipresence
			for _, proctype := range f.Release.Processes {
				if proctype.Omni {
					c.omniMtx.Lock()
					c.omni[f] = struct{}{}
					c.omniMtx.Unlock()
					break
				}
			}
			go func() {
				f.Rectify()
				if events != nil {
					events <- &FormationEvent{Formation: f}
				}
			}()
		}
		if *err != nil {
			g.Log(grohl.Data{"at": "error", "error": *err})
		}
		g.Log(grohl.Data{"at": "disconnect"})
		updates.Close()
	}
}

func (c *context) watchHosts(events chan<- *host.Event) {
	hosts, err := c.ListHosts()
	if err != nil {
		// TODO: log/handle error
	}

	go func() { // watch for new hosts
		ch := make(chan *host.HostEvent)
		c.StreamHostEvents(ch)
		for event := range ch {
			if event.Event != "add" {
				continue
			}
			go c.watchHost(event.HostID, events)

			c.omniMtx.RLock()
			for f := range c.omni {
				go f.Rectify()
			}
			c.omniMtx.RUnlock()
		}
	}()

	for id := range hosts {
		go c.watchHost(id, events)
	}

}

var putJobAttempts = attempt.Strategy{
	Total: 30 * time.Second,
	Delay: 500 * time.Millisecond,
}

func (c *context) watchHost(id string, events chan<- *host.Event) {
	if !c.hosts.Add(id) {
		return
	}
	defer c.hosts.Remove(id)

	g := grohl.NewContext(grohl.Data{"fn": "watchHost", "host.id": id})

	h, err := c.DialHost(id)
	if err != nil {
		// TODO: log/handle error
	}
	c.hosts.Set(id, h)

	g.Log(grohl.Data{"at": "start"})

	ch := make(chan *host.Event)
	h.StreamEvents("all", ch)

	// Nil event to mark the start of watching a host
	if events != nil {
		events <- nil
	}

	for event := range ch {
		job := c.jobs.Get(id, event.JobID)
		if job == nil {
			continue
		}

		j := &ct.Job{ID: id + "-" + event.JobID, AppID: job.Formation.AppID, ReleaseID: job.Formation.Release.ID, Type: job.Type}
		switch event.Event {
		case "create":
			j.State = "starting"
		case "start":
			j.State = "up"
			job.startedAt = event.Job.StartedAt
		case "stop":
			j.State = "down"
		case "error":
			j.State = "crashed"
		}
		g.Log(grohl.Data{"at": "event", "job.id": event.JobID, "event": event.Event})

		// Call PutJob in a goroutine as it may be the controller which has died
		go func(event *host.Event) {
			putJobAttempts.Run(func() error {
				if err := c.PutJob(j); err != nil {
					g.Log(grohl.Data{"at": "error", "job.id": event.JobID, "event": event.Event, "err": err})
					return err
				}
				g.Log(grohl.Data{"at": "put_job", "job.id": event.JobID, "event": event.Event})
				return nil
			})
		}(event)

		if event.Event != "error" && event.Event != "stop" {
			if events != nil {
				events <- event
			}
			continue
		}
		g.Log(grohl.Data{"at": "remove", "job.id": event.JobID, "event": event.Event})

		c.jobs.Remove(id, event.JobID)
		go func(event *host.Event) {
			c.mtx.RLock()
			job.Formation.RestartJob(job.Type, id, event.JobID)
			c.mtx.RUnlock()
			if events != nil {
				events <- event
			}
		}(event)
	}
	// TODO: check error/reconnect
}

func newHostClients() *hostClients {
	return &hostClients{hosts: make(map[string]cluster.Host)}
}

type hostClients struct {
	hosts map[string]cluster.Host
	mtx   sync.RWMutex
}

func (h *hostClients) Add(id string) bool {
	h.mtx.Lock()
	defer h.mtx.Unlock()
	if _, exists := h.hosts[id]; exists {
		return false
	}
	h.hosts[id] = nil
	return true
}

func (h *hostClients) Set(id string, client cluster.Host) {
	h.mtx.Lock()
	h.hosts[id] = client
	h.mtx.Unlock()
}

func (h *hostClients) Remove(id string) {
	h.mtx.Lock()
	delete(h.hosts, id)
	h.mtx.Unlock()
}

func (h *hostClients) Get(id string) cluster.Host {
	h.mtx.RLock()
	defer h.mtx.RUnlock()
	return h.hosts[id]
}

func newJobMap() *jobMap {
	return &jobMap{jobs: make(map[jobKey]*Job)}
}

type jobMap struct {
	jobs map[jobKey]*Job
	mtx  sync.RWMutex
}

func (m *jobMap) Add(job *Job) {
	m.mtx.Lock()
	m.jobs[jobKey{job.HostID, job.ID}] = job
	m.mtx.Unlock()
}

func (m *jobMap) Remove(host, job string) {
	m.mtx.Lock()
	delete(m.jobs, jobKey{host, job})
	m.mtx.Unlock()
}

func (m *jobMap) Get(host, job string) *Job {
	m.mtx.RLock()
	defer m.mtx.RUnlock()
	return m.jobs[jobKey{host, job}]
}

func (m *jobMap) Len() int {
	m.mtx.RLock()
	defer m.mtx.RUnlock()
	return len(m.jobs)
}

type jobKey struct {
	hostID, jobID string
}

type formationKey struct {
	appID, releaseID string
}

func NewFormations() *Formations {
	return &Formations{formations: make(map[formationKey]*Formation)}
}

type Formations struct {
	formations map[formationKey]*Formation
	mtx        sync.RWMutex
}

func (fs *Formations) Get(appID, releaseID string) *Formation {
	fs.mtx.RLock()
	defer fs.mtx.RUnlock()
	return fs.formations[formationKey{appID, releaseID}]
}

func (fs *Formations) Add(f *Formation) *Formation {
	fs.mtx.Lock()
	defer fs.mtx.Unlock()
	if existing, ok := fs.formations[f.key()]; ok {
		return existing
	}
	fs.formations[f.key()] = f
	return f
}

func (fs *Formations) Delete(f *Formation) {
	fs.mtx.Lock()
	delete(fs.formations, f.key())
	fs.mtx.Unlock()
}

func (fs *Formations) Len() int {
	fs.mtx.Lock()
	defer fs.mtx.Unlock()
	return len(fs.formations)
}

func NewFormation(c *context, ef *ct.ExpandedFormation) *Formation {
	return &Formation{
		AppID:     ef.App.ID,
		AppName:   ef.App.Name,
		Release:   ef.Release,
		Artifact:  ef.Artifact,
		Processes: ef.Processes,
		jobs:      make(jobTypeMap),
		c:         c,
	}
}

type Job struct {
	ID        string
	HostID    string
	Type      string
	Formation *Formation

	restarts  int
	timer     *time.Timer
	startedAt time.Time
}

type jobTypeMap map[string]map[jobKey]*Job

func (m jobTypeMap) Add(typ, host, id string) *Job {
	jobs, ok := m[typ]
	if !ok {
		jobs = make(map[jobKey]*Job)
		m[typ] = jobs
	}
	job := &Job{ID: id, HostID: host, Type: typ}
	jobs[jobKey{host, id}] = job
	return job
}

func (m jobTypeMap) Remove(job *Job) {
	if jobs, ok := m[job.Type]; ok {
		delete(jobs, jobKey{job.HostID, job.ID})
	}
}

func (m jobTypeMap) Get(typ, host, id string) *Job {
	return m[typ][jobKey{host, id}]
}

type Formation struct {
	mtx       sync.Mutex
	AppID     string
	AppName   string
	Release   *ct.Release
	Artifact  *ct.Artifact
	Processes map[string]int

	jobs jobTypeMap
	c    *context
}

func (f *Formation) key() formationKey {
	return formationKey{f.AppID, f.Release.ID}
}

func (f *Formation) SetProcesses(p map[string]int) {
	f.mtx.Lock()
	f.Processes = p
	f.mtx.Unlock()
}

func (f *Formation) Rectify() {
	f.mtx.Lock()
	defer f.mtx.Unlock()
	f.rectify()
}

func (f *Formation) RestartJob(typ, hostID, jobID string) {
	f.mtx.Lock()
	defer f.mtx.Unlock()

	job := f.jobs.Get(typ, hostID, jobID)
	if job == nil {
		return
	}
	// If it's a one off job, just remove it
	if job.Type == "" {
		f.jobs.Remove(job)
		return
	}
	// If the job was started more than backoffPeriod ago, reset it's restart count
	// so that it will be restarted straight away
	if job.startedAt.Before(time.Now().Add(-backoffPeriod)) {
		job.restarts = 0
	}
	if job.restarts == 0 {
		f.restart(job)
	} else {
		// wait backoffPeriod * 2 ^ (restarts - 1) before restarting
		duration := backoffPeriod
		for i := 0; i < job.restarts-1; i++ {
			duration *= 2
		}
		job.timer = timeAfterFunc(duration, func() {
			f.restart(job)
		})
	}
}

func (f *Formation) rectify() {
	g := grohl.NewContext(grohl.Data{"fn": "rectify", "app.id": f.AppID, "release.id": f.Release.ID})

	var hosts map[string]host.Host
	if _, ok := f.c.omni[f]; ok {
		var err error
		hosts, err = f.c.ListHosts()
		if err != nil {
			return
		}
		if len(hosts) == 0 {
			// TODO: log/handle error
		}
	}
	// update job counts
	for t, expected := range f.Processes {
		if f.Release.Processes[t].Omni {
			// get job counts per host
			hostCounts := make(map[string]int, len(hosts))
			for _, h := range hosts {
				hostCounts[h.ID] = 0
				for _, job := range h.Jobs {
					if f.jobType(job) != t {
						continue
					}
					hostCounts[h.ID]++
				}
			}
			// update per host
			for hostID, actual := range hostCounts {
				diff := expected - actual
				g.Log(grohl.Data{"at": "update", "type": t, "expected": expected, "actual": actual, "diff": diff})
				if diff > 0 {
					f.add(diff, t, hostID)
				} else if diff < 0 {
					f.remove(-diff, t, hostID)
				}
			}
		} else {
			actual := len(f.jobs[t])
			diff := expected - actual
			g.Log(grohl.Data{"at": "update", "type": t, "expected": expected, "actual": actual, "diff": diff})
			if diff > 0 {
				f.add(diff, t, "")
			} else if diff < 0 {
				f.remove(-diff, t, "")
			}
		}
	}

	// remove process types
	for t, jobs := range f.jobs {
		// ignore one-off jobs which have no type
		if t == "" {
			continue
		}
		if _, exists := f.Processes[t]; !exists {
			g.Log(grohl.Data{"at": "cleanup", "type": t, "count": len(jobs)})
			f.remove(len(jobs), t, "")
		}
	}
}

func (f *Formation) add(n int, name string, hostID string) {
	g := grohl.NewContext(grohl.Data{"fn": "add", "app.id": f.AppID, "release.id": f.Release.ID})
	for i := 0; i < n; i++ {
		job, err := f.start(name, hostID)
		if err != nil {
			// TODO: handle error
			g.Log(grohl.Data{"at": "error", "host.id": job.HostID, "job.id": job.ID, "err": err})
			continue
		}
		g.Log(grohl.Data{"at": "started", "host.id": job.HostID, "job.id": job.ID})
	}
}

func (f *Formation) restart(stoppedJob *Job) error {
	g := grohl.NewContext(grohl.Data{"fn": "restart", "app.id": f.AppID, "release.id": f.Release.ID})
	g.Log(grohl.Data{"old.host.id": stoppedJob.HostID, "old.job.id": stoppedJob.ID})

	f.jobs.Remove(stoppedJob)

	var hostID string
	if f.Release.Processes[stoppedJob.Type].Omni {
		hostID = stoppedJob.HostID
	}
	newJob, err := f.start(stoppedJob.Type, hostID)
	if err != nil {
		return err
	}
	newJob.restarts = stoppedJob.restarts + 1
	g.Log(grohl.Data{"new.host.id": newJob.HostID, "new.job.id": newJob.ID})
	return nil
}

func (f *Formation) start(typ string, hostID string) (job *Job, err error) {
	config := f.jobConfig(typ)
	config.ID = cluster.RandomJobID("")

	hosts, err := f.c.ListHosts()
	if err != nil {
		return nil, err
	}
	if len(hosts) == 0 {
		// TODO: log/handle error
	}
	var h host.Host

	if hostID != "" {
		h = hosts[hostID]
	} else {
		hostCounts := make(map[string]int, len(hosts))
		for _, h := range hosts {
			hostCounts[h.ID] = 0
			for _, job := range h.Jobs {
				if f.jobType(job) != typ {
					continue
				}
				hostCounts[h.ID]++
			}
		}
		sh := make(sortHosts, 0, len(hosts))
		for id, count := range hostCounts {
			sh = append(sh, sortHost{id, count})
		}
		sh.Sort()

		h = hosts[sh[0].ID]
	}

	job = f.jobs.Add(typ, h.ID, config.ID)
	job.Formation = f
	f.c.jobs.Add(job)

	_, err = f.c.AddJobs(&host.AddJobsReq{HostJobs: map[string][]*host.Job{h.ID: {config}}})
	if err != nil {
		f.jobs.Remove(job)
		f.c.jobs.Remove(config.ID, h.ID)
		return nil, err
	}
	return job, nil
}

func (f *Formation) jobType(job *host.Job) string {
	if job.Metadata["flynn-controller.app"] != f.AppID ||
		job.Metadata["flynn-controller.release"] != f.Release.ID {
		return ""
	}
	return job.Metadata["flynn-controller.type"]
}

func (f *Formation) remove(n int, name string, hostID string) {
	g := grohl.NewContext(grohl.Data{"fn": "remove", "app.id": f.AppID, "release.id": f.Release.ID})

	i := 0
	for _, job := range f.jobs[name] {
		g.Log(grohl.Data{"host.id": job.HostID, "job.id": job.ID})
		if hostID != "" && job.HostID != hostID { // remove from a specific host
			continue
		}
		// TODO: robust host handling
		if err := f.c.hosts.Get(job.HostID).StopJob(job.ID); err != nil {
			// TODO: log/handle error
		}
		f.jobs.Remove(job)
		if i++; i == n {
			break
		}
	}
}

func (f *Formation) jobConfig(name string) *host.Job {
	return utils.JobConfig(&ct.ExpandedFormation{
		App:      &ct.App{ID: f.AppID, Name: f.AppName},
		Release:  f.Release,
		Artifact: f.Artifact,
	}, name)
}

type sortHost struct {
	ID   string
	Jobs int
}

type sortHosts []sortHost

func (h sortHosts) Len() int           { return len(h) }
func (h sortHosts) Less(i, j int) bool { return h[i].Jobs < h[j].Jobs }
func (h sortHosts) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h sortHosts) Sort()              { sort.Sort(h) }

type FormationEvent struct {
	Formation *Formation
}
