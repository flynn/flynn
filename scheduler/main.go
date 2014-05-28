package main

import (
	"log"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/flynn/flynn-controller/client"
	ct "github.com/flynn/flynn-controller/types"
	"github.com/flynn/flynn-controller/utils"
	"github.com/flynn/flynn-host/types"
	"github.com/flynn/go-discoverd"
	"github.com/flynn/go-flynn/cluster"
	"github.com/technoweenie/grohl"
)

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
	c.watchFormations(nil)
}

func newContext(cc controllerClient, cl clusterClient) *context {
	return &context{
		controllerClient: cc,
		clusterClient:    cl,
		formations:       NewFormations(),
		hosts:            newHostClients(),
		jobs:             newJobMap(),
	}
}

type context struct {
	controllerClient
	clusterClient
	formations *Formations

	hosts *hostClients
	jobs  *jobMap
	mtx   sync.RWMutex
}

type clusterClient interface {
	ListHosts() (map[string]host.Host, error)
	AddJobs(req *host.AddJobsReq) (*host.AddJobsRes, error)
	DialHost(id string) (cluster.Host, error)
}

type controllerClient interface {
	GetRelease(releaseID string) (*ct.Release, error)
	GetArtifact(artifactID string) (*ct.Artifact, error)
	GetFormation(appID, releaseID string) (*ct.Formation, error)
	StreamFormations(since *time.Time) (<-chan *ct.ExpandedFormation, *error)
}

func (c *context) syncCluster(events chan<- *JobRemovalEvent) {
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
			appID := job.Attributes["flynn-controller.app"]
			releaseID := job.Attributes["flynn-controller.release"]
			jobType := job.Attributes["flynn-controller.type"]
			gg := g.New(grohl.Data{"host.id": h.ID, "job.id": job.ID, "app.id": appID, "release.id": releaseID, "type": jobType})

			if appID == "" || releaseID == "" || jobType == "" {
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
					App:       &ct.App{ID: appID},
					Release:   release,
					Artifact:  artifact,
					Processes: formation.Processes,
				})
				gg.Log(grohl.Data{"at": "addFormation"})
				f = c.formations.Add(f)
			}

			gg.Log(grohl.Data{"at": "addJob"})
			j := f.jobs.Add(jobType, h.ID, job.ID)
			j.Formation = f
			c.jobs.Add(h.ID, job.ID, j)
			rectify[f] = struct{}{}
		}
	}
	c.mtx.Unlock()

	for f := range rectify {
		go f.rectify()
	}
}

func (c *context) watchFormations(events chan<- *FormationEvent) {
	g := grohl.NewContext(grohl.Data{"fn": "watchFormations"})

	ch, _ := c.StreamFormations(nil)

	c.syncCluster(nil)
	if events != nil {
		events <- &FormationEvent{}
	}

	for ef := range ch {
		if ef.App == nil {
			// sentinel
			continue
		}
		f := c.formations.Get(ef.App.ID, ef.Release.ID)
		if f != nil {
			g.Log(grohl.Data{"app.id": ef.App.ID, "release.id": ef.Release.ID, "at": "update"})
			f.SetProcesses(ef.Processes)
		} else {
			g.Log(grohl.Data{"app.id": ef.App.ID, "release.id": ef.Release.ID, "at": "new"})
			f = NewFormation(c, ef)
			c.formations.Add(f)
		}
		go func() {
			f.Rectify()
			if events != nil {
				events <- &FormationEvent{Formation: f}
			}
		}()
	}

	// TODO: log disconnect and restart
	// TODO: trigger cluster sync
}

func (c *context) watchHosts(events chan<- *JobRemovalEvent) {
	hosts, err := c.ListHosts()
	if err != nil {
		// TODO: log/handle error
	}

	for id, _ := range hosts {
		go c.watchHost(id, events)
	}

}

func (c *context) watchHost(id string, events chan<- *JobRemovalEvent) {
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
	for event := range ch {
		if event.Event != "error" && event.Event != "stop" {
			continue
		}
		job := c.jobs.Get(id, event.JobID)
		if job == nil {
			continue
		}
		g.Log(grohl.Data{"at": "remove", "job.id": event.JobID, "event": event.Event})

		c.jobs.Remove(id, event.JobID)
		go func() {
			c.mtx.RLock()
			job.Formation.RemoveJob(job.Type, id, event.JobID)
			c.mtx.RUnlock()
			if events != nil {
				events <- &JobRemovalEvent{JobID: event.JobID}
			}
		}()
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

func (m *jobMap) Add(hostID, jobID string, job *Job) {
	m.mtx.Lock()
	m.jobs[jobKey{hostID, jobID}] = job
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
		Release:   ef.Release,
		Artifact:  ef.Artifact,
		Processes: ef.Processes,
		jobs:      make(jobTypeMap),
		c:         c,
	}
}

type Job struct {
	Type      string
	Formation *Formation
}

type jobTypeMap map[string]map[jobKey]*Job

func (m jobTypeMap) Add(typ, host, id string) *Job {
	jobs, ok := m[typ]
	if !ok {
		jobs = make(map[jobKey]*Job)
		m[typ] = jobs
	}
	job := &Job{Type: typ}
	jobs[jobKey{host, id}] = job
	return job
}

func (m jobTypeMap) Remove(typ, host, id string) {
	if jobs, ok := m[typ]; ok {
		delete(jobs, jobKey{host, id})
	}
}

func (m jobTypeMap) Get(typ, host, id string) *Job {
	return m[typ][jobKey{host, id}]
}

type Formation struct {
	mtx       sync.Mutex
	AppID     string
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

func (f *Formation) RemoveJob(typ, hostID, jobID string) {
	f.mtx.Lock()
	defer f.mtx.Unlock()

	f.jobs.Remove(typ, hostID, jobID)
	f.rectify()
}

func (f *Formation) rectify() {
	g := grohl.NewContext(grohl.Data{"fn": "rectify", "app.id": f.AppID, "release.id": f.Release.ID})

	// update job counts
	for t, expected := range f.Processes {
		diff := expected - len(f.jobs[t])
		g.Log(grohl.Data{"at": "update", "type": t, "expected": expected, "actual": len(f.jobs[t]), "diff": diff})
		if diff > 0 {
			f.add(diff, t)
		} else if diff < 0 {
			f.remove(-diff, t)
		}
	}

	// remove process types
	for t, jobs := range f.jobs {
		if _, exists := f.Processes[t]; !exists {
			g.Log(grohl.Data{"at": "cleanup", "type": t, "count": len(jobs)})
			f.remove(len(jobs), t)
		}
	}
}

func (f *Formation) add(n int, name string) {
	g := grohl.NewContext(grohl.Data{"fn": "add", "app.id": f.AppID, "release.id": f.Release.ID})

	config, err := f.jobConfig(name)
	if err != nil {
		// TODO: log/handle error
	}
	for i := 0; i < n; i++ {
		config.ID = cluster.RandomJobID("")
		hosts, err := f.c.ListHosts()
		if err != nil {
			// TODO: log/handle error
		}
		if len(hosts) == 0 {
			// TODO: log/handle error
		}
		hostCounts := make(map[string]int, len(hosts))
		for _, h := range hosts {
			hostCounts[h.ID] = 0
			for _, job := range h.Jobs {
				if f.jobType(job) != name {
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

		h := hosts[sh[0].ID]

		g.Log(grohl.Data{"host.id": h.ID, "job.id": config.ID})

		job := f.jobs.Add(name, h.ID, config.ID)
		job.Formation = f
		f.c.jobs.Add(h.ID, config.ID, job)

		_, err = f.c.AddJobs(&host.AddJobsReq{HostJobs: map[string][]*host.Job{h.ID: {config}}})
		if err != nil {
			f.jobs.Remove(name, h.ID, config.ID)
			f.c.jobs.Remove(h.ID, config.ID)
			// TODO: log/handle error
		}
	}
}

func (f *Formation) jobType(job *host.Job) string {
	if job.Attributes["flynn-controller.app"] != f.AppID ||
		job.Attributes["flynn-controller.release"] != f.Release.ID {
		return ""
	}
	return job.Attributes["flynn-controller.type"]
}

func (f *Formation) remove(n int, name string) {
	g := grohl.NewContext(grohl.Data{"fn": "remove", "app.id": f.AppID, "release.id": f.Release.ID})

	i := 0
	for k := range f.jobs[name] {
		g.Log(grohl.Data{"host.id": k.hostID, "job.id": k.jobID})
		// TODO: robust host handling
		if err := f.c.hosts.Get(k.hostID).StopJob(k.jobID); err != nil {
			// TODO: log/handle error
		}
		f.jobs.Remove(name, k.hostID, k.jobID)
		f.c.jobs.Remove(k.hostID, k.jobID)
		if i++; i == n {
			break
		}
	}
}

func (f *Formation) jobConfig(name string) (*host.Job, error) {
	return utils.JobConfig(&ct.ExpandedFormation{
		App:      &ct.App{ID: f.AppID},
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

type JobRemovalEvent struct {
	JobID string
}
