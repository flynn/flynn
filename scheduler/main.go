package main

import (
	"errors"
	"log"
	"net/url"
	"os"
	"sort"
	"sync"

	"github.com/flynn/flynn-controller/client"
	ct "github.com/flynn/flynn-controller/types"
	"github.com/flynn/flynn-host/types"
	"github.com/flynn/go-dockerclient"
	"github.com/flynn/go-flynn/cluster"
)

func main() {
	controller, err := client.New(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}
	cl, err := cluster.NewClient()
	if err != nil {
		log.Fatal(err)
	}
	c := newContext(cl)
	// TODO: initial load of data
	// TODO: periodic full cluster sync for anti-entropy
	go c.watchFormations(controller)
}

func newContext(cl clusterClient) *context {
	return &context{
		clusterClient: cl,
		formations:    NewFormations(),
		hosts:         newHostClients(),
		jobs:          newJobMap(),
	}
}

type context struct {
	clusterClient
	formations *Formations

	hosts *hostClients
	jobs  *jobMap
}

type clusterClient interface {
	ListHosts() (map[string]host.Host, error)
	AddJobs(req *host.AddJobsReq) (*host.AddJobsRes, error)
	ConnectHost(id string) (cluster.Host, error)
}

type formationStreamer interface {
	StreamFormations(chan<- *ct.ExpandedFormation) *error
}

func (c *context) watchFormations(fs formationStreamer) {
	ch := make(chan *ct.ExpandedFormation)
	fs.StreamFormations(ch)

	for ef := range ch {
		f := NewFormation(c, ef)
		c.formations.Add(f)
		go f.Rectify()
	}

	// TODO: log disconnect and restart
	// TODO: trigger cluster sync
}

func (c *context) watchHost(id string) {
	if !c.hosts.Add(id) {
		return
	}
	defer c.hosts.Remove(id)

	h, err := c.ConnectHost(id)
	if err != nil {
		// TODO: log/handle error
	}
	c.hosts.Set(id, h)

	ch := make(chan host.Event)
	h.StreamEvents("all", ch)
	for event := range ch {
		if event.Event != "error" && event.Event != "stop" {
			continue
		}
		job := c.jobs.Get(id, event.JobID)
		if job == nil {
			continue
		}

		c.jobs.Remove(id, event.JobID)
		go job.Formation.RemoveJob(job.Type, id, event.JobID)
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

func (fs *Formations) Add(f *Formation) {
	fs.mtx.Lock()
	fs.formations[f.key()] = f
	fs.mtx.Unlock()
}

func (fs *Formations) Delete(f *Formation) {
	fs.mtx.Lock()
	delete(fs.formations, f.key())
	fs.mtx.Unlock()
}

func NewFormation(c *context, ef *ct.ExpandedFormation) *Formation {
	return &Formation{
		App:       ef.App,
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
	App       *ct.App
	Release   *ct.Release
	Artifact  *ct.Artifact
	Processes map[string]int

	jobs jobTypeMap
	c    *context
}

func (f *Formation) key() formationKey {
	return formationKey{f.App.ID, f.Release.ID}
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
	// update job counts
	for t, expected := range f.Processes {
		diff := expected - len(f.jobs[t])
		if diff > 0 {
			f.add(diff, t)
		} else if diff < 0 {
			f.remove(-diff, t)
		}
	}

	// remove process types
	for t, jobs := range f.jobs {
		if _, exists := f.Processes[t]; !exists {
			f.remove(len(jobs), t)
		}
	}
}

func (f *Formation) add(n int, name string) {
	config, err := f.jobConfig(name)
	if err != nil {
		// TODO: log/handle error
	}
	for i := 0; i < n; i++ {
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
		go f.c.watchHost(h.ID)

		job := f.jobs.Add(name, h.ID, config.ID)
		job.Formation = f
		f.c.jobs.Add(h.ID, config.ID, job)

		res, err := f.c.AddJobs(&host.AddJobsReq{HostJobs: map[string][]*host.Job{h.ID: {config}}})
		if err != nil || !res.Success {
			f.jobs.Remove(name, h.ID, config.ID)
			f.c.jobs.Remove(h.ID, config.ID)
			// TODO: log/handle error
		}
	}
}

func (f *Formation) jobType(job *host.Job) string {
	if job.Attributes["flynn-controller.app"] != f.App.ID ||
		job.Attributes["flynn-controller.release"] != f.Release.ID {
		return ""
	}
	return job.Attributes["flynn-controller.type"]
}

func (f *Formation) remove(n int, name string) {
	i := 0
	for k := range f.jobs[name] {
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
	t := f.Release.Processes[name]
	image, err := dockerImage(f.Artifact.URI)
	if err != nil {
		return nil, err
	}
	return &host.Job{
		ID:       cluster.RandomJobID(""),
		TCPPorts: t.Ports.TCP,
		Attributes: map[string]string{
			"flynn-controller.app":     f.App.ID,
			"flynn-controller.release": f.Release.ID,
			"flynn-controller.type":    name,
		},
		Config: &docker.Config{
			Cmd:   t.Cmd,
			Env:   formatEnv(f.Release.Env, t.Env),
			Image: image,
		},
	}, nil
}

func dockerImage(uri string) (string, error) {
	// TODO: ID refs (see https://github.com/dotcloud/docker/issues/4106)
	u, err := url.Parse(uri)
	if err != nil {
		return "", err
	}
	if u.Scheme != "docker" {
		return "", errors.New("scheduler: only docker artifact URIs are currently supported")
	}
	var suffix string
	if tag := u.Query().Get("tag"); tag != "" {
		suffix = ":" + tag
	}
	return u.Host + suffix, nil
}

func formatEnv(envs ...map[string]string) []string {
	env := make(map[string]string)
	for _, e := range envs {
		for k, v := range e {
			env[k] = v
		}
	}
	res := make([]string, 0, len(env))
	for k, v := range env {
		res = append(res, k+"="+v)
	}
	return res
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
