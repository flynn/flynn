package main

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/technoweenie/grohl"
	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/controller/utils"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/pkg/attempt"
	"github.com/flynn/flynn/pkg/cluster"
	"github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/shutdown"
	"github.com/flynn/flynn/pkg/status"
	"github.com/flynn/flynn/pkg/stream"
)

var backoffPeriod = 10 * time.Minute

func main() {
	defer shutdown.Exit()

	grohl.AddContext("app", "controller-scheduler")
	grohl.Log(grohl.Data{"at": "start"})

	go startHTTPServer()

	if period := os.Getenv("BACKOFF_PERIOD"); period != "" {
		var err error
		backoffPeriod, err = time.ParseDuration(period)
		if err != nil {
			shutdown.Fatal(err)
		}
		grohl.Log(grohl.Data{"at": "backoff_period", "period": backoffPeriod.String()})
	}

	cc, err := controller.NewClient("", os.Getenv("AUTH_KEY"))
	if err != nil {
		shutdown.Fatal(err)
	}
	c := newContext(cc, cluster.NewClient())

	c.watchHosts()

	grohl.Log(grohl.Data{"at": "leaderwait"})
	hb, err := discoverd.AddServiceAndRegister("flynn-controller-scheduler", ":"+os.Getenv("PORT"))
	if err != nil {
		shutdown.Fatal(err)
	}
	shutdown.BeforeExit(func() { hb.Close() })

	leaders := make(chan *discoverd.Instance)
	stream, err := discoverd.NewService("flynn-controller-scheduler").Leaders(leaders)
	if err != nil {
		shutdown.Fatal(err)
	}
	for leader := range leaders {
		if leader.Addr == hb.Addr() {
			break
		}
	}
	if err := stream.Err(); err != nil {
		// TODO: handle discoverd errors
		shutdown.Fatal(err)
	}
	stream.Close()
	// TODO: handle demotion

	grohl.Log(grohl.Data{"at": "leader"})

	// TODO: periodic full cluster sync for anti-entropy
	c.watchFormations()
}

func startHTTPServer() {
	status.AddHandler(status.HealthyHandler)
	shutdown.Fatal(http.ListenAndServe(":"+os.Getenv("PORT"), nil))
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
	Hosts() ([]*cluster.Host, error)
	Host(string) (*cluster.Host, error)
	StreamHosts(chan *cluster.Host) (stream.Stream, error)
}

type controllerClient interface {
	AppList() ([]*ct.App, error)
	JobList(string) ([]*ct.Job, error)
	GetRelease(releaseID string) (*ct.Release, error)
	GetArtifact(artifactID string) (*ct.Artifact, error)
	GetFormation(appID, releaseID string) (*ct.Formation, error)
	StreamFormations(since *time.Time, output chan<- *ct.ExpandedFormation) (stream.Stream, error)
	PutJob(job *ct.Job) error
}

func jobMetaFromMetadata(metadata map[string]string) map[string]string {
	jobMeta := make(map[string]string, len(metadata))
	for k, v := range metadata {
		if strings.HasPrefix(k, "flynn-controller.") {
			continue
		}
		jobMeta[k] = v
	}
	return jobMeta
}

func (c *context) syncCluster() {
	g := grohl.NewContext(grohl.Data{"fn": "syncCluster"})

	artifacts := make(map[string]*ct.Artifact)
	releases := make(map[string]*ct.Release)
	rectify := make(map[*Formation]struct{})

	hosts, err := c.Hosts()
	if err != nil {
		// TODO: log/handle error
	}

	c.mtx.Lock()
	for _, h := range hosts {
		jobs, err := h.ListJobs()
		if err != nil {
			// TODO: log/handle error
			continue
		}
		for _, j := range jobs {
			if j.Status != host.StatusStarting && j.Status != host.StatusRunning {
				continue
			}
			job := j.Job
			appID := job.Metadata["flynn-controller.app"]
			appName := job.Metadata["flynn-controller.app_name"]
			releaseID := job.Metadata["flynn-controller.release"]
			jobType := job.Metadata["flynn-controller.type"]
			gg := g.New(grohl.Data{"host.id": h.ID(), "job.id": job.ID, "app.id": appID, "release.id": releaseID, "type": jobType})

			if appID == "" || releaseID == "" {
				continue
			}
			if job := c.jobs.Get(h.ID(), job.ID); job != nil {
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
				ID:        job.ID,
				AppID:     appID,
				ReleaseID: releaseID,
				Type:      jobType,
				State:     "up",
				Meta:      jobMetaFromMetadata(job.Metadata),
			})
			j := f.jobs.Add(jobType, h.ID(), job.ID)
			j.Formation = f
			c.jobs.Add(j)
			rectify[f] = struct{}{}
		}
	}
	if err := c.syncJobStates(); err != nil {
		// TODO: handle error
	}
	c.mtx.Unlock()

	for f := range rectify {
		go f.Rectify()
	}
}

func (c *context) syncJobStates() error {
	g := grohl.NewContext(grohl.Data{"fn": "syncJobStates"})
	g.Log(grohl.Data{"at": "appList"})
	apps, err := c.AppList()
	if err != nil {
		g.Log(grohl.Data{"at": "appList", "status": "error", "err": err})
		return err
	}
	for _, app := range apps {
		g.Log(grohl.Data{"at": "jobList", "app.id": app.ID})
		jobs, err := c.JobList(app.ID)
		if err != nil {
			g.Log(grohl.Data{"at": "jobList", "app.id": app.ID, "status": "error", "err": err})
			continue
		}
		for _, job := range jobs {
			gg := g.New(grohl.Data{"job.id": job.ID, "app.id": app.ID, "state": job.State})
			gg.Log(grohl.Data{"at": "checkState"})
			if job.State != "up" {
				continue
			}
			hostID, err := cluster.ExtractHostID(job.ID)
			if err != nil {
				gg.Log(grohl.Data{"at": "jobHostID", "status": "error", "err": err})
				continue
			}
			if j := c.jobs.Get(hostID, job.ID); j != nil {
				continue
			}
			job.State = "down"
			gg.Log(grohl.Data{"at": "putJob", "state": "down"})
			go c.PutJob(job)
		}
	}
	return nil
}

func (c *context) watchFormations() {
	g := grohl.NewContext(grohl.Data{"fn": "watchFormations"})

	c.syncCluster()

	var attempts int
	var lastUpdatedAt time.Time
	for {
		// wait a second if we've tried more than once
		attempts++
		if attempts > 1 {
			time.Sleep(time.Second)
		}

		g.Log(grohl.Data{"at": "connect", "attempt": attempts})
		updates := make(chan *ct.ExpandedFormation)
		streamCtrl, err := c.StreamFormations(&lastUpdatedAt, updates)
		if err != nil {
			g.Log(grohl.Data{"at": "error", "error": err})
			continue
		}
		for ef := range updates {
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
			go f.Rectify()
		}
		if streamCtrl.Err() != nil {
			g.Log(grohl.Data{"at": "disconnect", "err": streamCtrl.Err()})
		}
		g.Log(grohl.Data{"at": "disconnect"})
	}
}

func (c *context) watchHosts() {
	ready := make(chan struct{})
	go func() { // watch for new hosts
		ch := make(chan *cluster.Host)
		_, err := c.StreamHosts(ch)
		if err != nil {
			panic(err)
		}
		for h := range ch {
			if h == nil {
				close(ready)
				continue
			}

			go c.watchHost(h, nil)

			c.omniMtx.RLock()
			for f := range c.omni {
				go f.Rectify()
			}
			c.omniMtx.RUnlock()
		}
	}()

	<-ready
}

var putJobAttempts = attempt.Strategy{
	Total: 30 * time.Second,
	Delay: 500 * time.Millisecond,
}

func jobState(event *host.Event) string {
	switch event.Job.Status {
	case host.StatusStarting:
		return "starting"
	case host.StatusRunning:
		return "up"
	case host.StatusDone:
		return "down"
	case host.StatusCrashed:
		return "crashed"
	case host.StatusFailed:
		return "failed"
	default:
		return ""
	}
}

var dialHostAttempts = attempt.Strategy{
	Total: 60 * time.Second,
	Delay: 200 * time.Millisecond,
}

func (c *context) watchHost(h *cluster.Host, ready chan struct{}) {
	if !c.hosts.Add(h.ID()) {
		if ready != nil {
			ready <- struct{}{}
		}
		return
	}
	defer c.hosts.Remove(h.ID())

	g := grohl.NewContext(grohl.Data{"fn": "watchHost", "host.id": h.ID()})

	c.hosts.Set(h.ID(), h)

	g.Log(grohl.Data{"at": "start"})

	ch := make(chan *host.Event)
	h.StreamEvents("all", ch)
	if ready != nil {
		ready <- struct{}{}
	}

	// Call PutJob in a goroutine so we don't block receiving job events whilst potentially
	// making multiple requests to the controller (e.g. if the controller is down).
	//
	// Use a channel (rather than spawning a goroutine per event) so that events are delivered in order.
	jobs := make(chan *ct.Job, 10)
	go func() {
		for job := range jobs {
			putJobAttempts.Run(func() error {
				if err := c.PutJob(job); err != nil {
					g.Log(grohl.Data{"at": "put_job_error", "job.id": job.ID, "state": job.State, "err": err})
					// ignore validation / not found errors
					if httphelper.IsValidationError(err) || err == controller.ErrNotFound {
						return nil
					}
					return err
				}
				g.Log(grohl.Data{"at": "put_job", "job.id": job.ID, "state": job.State})
				return nil
			})
		}
	}()

	for event := range ch {
		meta := event.Job.Job.Metadata
		appID := meta["flynn-controller.app"]
		releaseID := meta["flynn-controller.release"]
		jobType := meta["flynn-controller.type"]

		if appID == "" || releaseID == "" {
			continue
		}

		job := &ct.Job{
			ID:        event.JobID,
			AppID:     appID,
			ReleaseID: releaseID,
			Type:      jobType,
			State:     jobState(event),
			Meta:      jobMetaFromMetadata(meta),
		}
		g.Log(grohl.Data{"at": "event", "job.id": event.JobID, "event": event.Event})
		jobs <- job

		// get a read lock on the mutex to ensure we are not currently
		// syncing with the cluster
		c.mtx.RLock()
		j := c.jobs.Get(h.ID(), event.JobID)
		c.mtx.RUnlock()
		if j == nil {
			continue
		}
		j.startedAt = event.Job.StartedAt

		if event.Event != "error" && event.Event != "stop" {
			continue
		}
		g.Log(grohl.Data{"at": "remove", "job.id": event.JobID, "event": event.Event})

		c.jobs.Remove(h.ID(), event.JobID)
		go func(event *host.Event) {
			c.mtx.RLock()
			j.Formation.RestartJob(jobType, h.ID(), event.JobID)
			c.mtx.RUnlock()
		}(event)
	}
	// TODO: check error/reconnect
}

func newHostClients() *hostClients {
	h := &hostClients{hosts: make(map[string]*cluster.Host)}
	h.hostList.Store([]*cluster.Host{})
	return h
}

type hostClients struct {
	hosts    map[string]*cluster.Host
	hostList atomic.Value // immutable []*cluster.Host
	mtx      sync.RWMutex
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

func (h *hostClients) Set(id string, client *cluster.Host) {
	h.mtx.Lock()
	h.hosts[id] = client
	h.updateHostList()
	h.mtx.Unlock()
}

func (h *hostClients) Remove(id string) {
	h.mtx.Lock()
	delete(h.hosts, id)
	h.updateHostList()
	h.mtx.Unlock()
}

func (h *hostClients) Get(id string) *cluster.Host {
	h.mtx.RLock()
	defer h.mtx.RUnlock()
	return h.hosts[id]
}

func (h *hostClients) List() []*cluster.Host {
	return h.hostList.Load().([]*cluster.Host)
}

func (h *hostClients) updateHostList() {
	hosts := make([]*cluster.Host, 0, len(h.hosts))
	for _, host := range h.hosts {
		hosts = append(hosts, host)
	}
	h.hostList.Store(hosts)
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
	timerMtx  sync.Mutex
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
		j := jobs[jobKey{job.HostID, job.ID}]
		// cancel job restarts
		j.timerMtx.Lock()
		if j.timer != nil {
			j.timer.Stop()
			j.timer = nil
		}
		j.timerMtx.Unlock()
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
		job.timerMtx.Lock()
		job.timer = time.AfterFunc(duration, func() {
			f.restart(job)
		})
		job.timerMtx.Unlock()
	}
}

func (f *Formation) rectify() {
	g := grohl.NewContext(grohl.Data{"fn": "rectify", "app.id": f.AppID, "release.id": f.Release.ID})

	var hosts []*cluster.Host
	if _, ok := f.c.omni[f]; ok {
		hosts = f.c.hosts.List()
	}
	// update job counts
	for t, expected := range f.Processes {
		if f.Release.Processes[t].Omni {
			// get job counts per host
			hostCounts := make(map[string]int, len(hosts))
			for _, h := range hosts {
				hostCounts[h.ID()] = 0
			}
			for k := range f.jobs[t] {
				hostCounts[k.hostID]++
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
			g.Log(grohl.Data{"at": "error", "host.id": hostID, "job.name": name, "err": err.Error()})
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
	if hostID == "" {
		hosts := f.c.hosts.List()
		if len(hosts) == 0 {
			return nil, errors.New("no hosts found")
		}
		sh := make(sortHosts, 0, len(hosts))
		for _, host := range hosts {
			count := 0
			for k := range f.jobs[typ] {
				if k.hostID == host.ID() {
					count++
				}
			}
			sh = append(sh, sortHost{host.ID(), count})
		}
		sh.Sort()
		hostID = sh[0].HostID
	}
	h := f.c.hosts.Get(hostID)
	if h == nil {
		return nil, fmt.Errorf("unknown host %q", hostID)
	}

	config := f.jobConfig(typ, h.ID())

	// Provision a data volume on the host if needed.
	if f.Release.Processes[typ].Data {
		if err := utils.ProvisionVolume(h, config); err != nil {
			return nil, err
		}
	}

	job = f.jobs.Add(typ, h.ID(), config.ID)
	job.Formation = f
	f.c.jobs.Add(job)

	if err := h.AddJob(config); err != nil {
		f.jobs.Remove(job)
		f.c.jobs.Remove(config.ID, h.ID())
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

// sortJobs sorts Jobs in reverse chronological order based on their startedAt time
type sortJobs []*Job

func (s sortJobs) Len() int { return len(s) }
func (s sortJobs) Less(i, j int) bool {
	s[i].timerMtx.Lock()
	s[j].timerMtx.Lock()
	defer s[i].timerMtx.Unlock()
	defer s[j].timerMtx.Unlock()
	switch {
	case s[i].timer != nil && s[j].timer == nil:
		return true
	case s[i].timer == nil && s[j].timer != nil:
		return false
	default:
		return s[i].startedAt.Sub(s[j].startedAt) > 0
	}
}
func (s sortJobs) Swap(i, j int) { s[i], s[j] = s[j], s[i] }
func (s sortJobs) Sort()         { sort.Sort(s) }

func (f *Formation) remove(n int, name string, hostID string) {
	g := grohl.NewContext(grohl.Data{"fn": "remove", "app.id": f.AppID, "release.id": f.Release.ID})

	i := 0
	sj := make(sortJobs, 0, len(f.jobs[name]))
	for _, job := range f.jobs[name] {
		sj = append(sj, job)
	}
	sj.Sort()
	for _, job := range sj {
		g.Log(grohl.Data{"host.id": job.HostID, "job.id": job.ID})
		if hostID != "" && job.HostID != hostID { // remove from a specific host
			continue
		}
		// TODO: robust host handling
		if err := f.c.hosts.Get(job.HostID).StopJob(job.ID); err != nil {
			g.Log(grohl.Data{"at": "error", "err": err.Error()})
			// TODO: handle error
		}
		f.jobs.Remove(job)
		if i++; i == n {
			break
		}
	}
}

func (f *Formation) jobConfig(name string, hostID string) *host.Job {
	return utils.JobConfig(&ct.ExpandedFormation{
		App:      &ct.App{ID: f.AppID, Name: f.AppName},
		Release:  f.Release,
		Artifact: f.Artifact,
	}, name, hostID)
}

type sortHost struct {
	HostID string
	Jobs   int
}

type sortHosts []sortHost

func (h sortHosts) Len() int      { return len(h) }
func (h sortHosts) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h sortHosts) Sort()         { sort.Sort(h) }

func (h sortHosts) Less(i, j int) bool {
	if h[i].Jobs == h[j].Jobs {
		return h[i].HostID < h[j].HostID
	}
	return h[i].Jobs < h[j].Jobs
}

type FormationEvent struct {
	Formation *Formation
}
