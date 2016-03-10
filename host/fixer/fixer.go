package fixer

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-docopt"
	"github.com/flynn/flynn/Godeps/_workspace/src/gopkg.in/inconshreveable/log15.v2"
	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/controller/utils"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/pkg/cluster"
	"github.com/flynn/flynn/pkg/sirenia/client"
	pgstate "github.com/flynn/flynn/pkg/sirenia/state"
)

type ClusterFixer struct {
	hosts []*cluster.Host
	c     *cluster.Client
	l     log15.Logger
}

func NewClusterFixer(hosts []*cluster.Host, c *cluster.Client, l log15.Logger) *ClusterFixer {
	return &ClusterFixer{
		hosts: hosts,
		c:     c,
		l:     l,
	}
}

func (f *ClusterFixer) Run(args *docopt.Args, c *cluster.Client) error {
	f.c = c
	f.l = log15.New()
	var err error

	minHosts, err := strconv.Atoi(args.String["--min-hosts"])
	if err != nil || minHosts < 1 {
		return fmt.Errorf("invalid or missing --min-hosts value")
	}

	f.hosts, err = c.Hosts()
	if err != nil {
		f.l.Error("unable to list hosts from discoverd, falling back to peer IP list", "error", err)
		var ips []string
		if ipList := args.String["--peer-ips"]; ipList != "" {
			ips = strings.Split(ipList, ",")
			if minHosts == 0 {
				minHosts = len(ips)
			}
		}
		if len(ips) == 0 {
			return fmt.Errorf("error connecting to discoverd, use --peer-ips: %s", err)
		}
		if len(ips) < minHosts {
			return fmt.Errorf("number of peer IPs provided (%d) is less than --min-hosts (%d)", len(ips), minHosts)
		}

		f.hosts = make([]*cluster.Host, len(ips))
		for i, ip := range ips {
			url := fmt.Sprintf("http://%s:1113", ip)
			status, err := cluster.NewHost("", url, nil, nil).GetStatus()
			if err != nil {
				return fmt.Errorf("error connecting to %s: %s", ip, err)
			}
			f.hosts[i] = cluster.NewHost(status.ID, url, nil, nil)
		}
	}
	// check expected number of hosts
	if len(f.hosts) < minHosts {
		// TODO(titanous): be smarter about this
		return fmt.Errorf("expected at least %d hosts, but %d found", minHosts, len(f.hosts))
	}
	f.l.Info("found expected hosts", "n", len(f.hosts))

	if err := f.FixDiscoverd(); err != nil {
		return err
	}
	if err := f.FixFlannel(); err != nil {
		return err
	}

	f.l.Info("waiting for discoverd to be available")
	timeout := time.After(time.Minute)
	for {
		var err error
		if _, err = discoverd.GetInstances("discoverd", 30*time.Second); err != nil {
			time.Sleep(100 * time.Millisecond)
		} else {
			break
		}
		select {
		case <-timeout:
			return fmt.Errorf("timed out waiting for discoverd, last error: %s", err)
		}
	}

	f.l.Info("checking for running controller API")
	controllerService := discoverd.NewService("controller")
	controllerInstances, _ := controllerService.Instances()
	if len(controllerInstances) > 0 {
		f.l.Info("found running controller API instances", "n", len(controllerInstances))
		if err := f.FixController(controllerInstances, false); err != nil {
			f.l.Error("error fixing controller", "err", err)
			// if unable to write correct formations, we need to kill the scheduler so that the rest of this works
			if err := f.KillSchedulers(); err != nil {
				return err
			}
		}
	}

	if err := f.FixPostgres(); err != nil {
		return err
	}

	f.l.Info("checking for running controller API")
	controllerInstances, _ = controllerService.Instances()
	if len(controllerInstances) == 0 {
		// kill schedulers to prevent interference
		if err := f.KillSchedulers(); err != nil {
			return err
		}
		controllerInstances, err = f.StartAppJob("controller", "web", "controller")
		if err != nil {
			return err
		}
	} else {
		f.l.Info("found running controller API instances", "n", len(controllerInstances))
	}

	if err := f.FixController(controllerInstances, true); err != nil {
		f.l.Error("error fixing controller", "err", err)
		return err
	}

	f.l.Info("cluster fix complete")

	return nil
}

func (f *ClusterFixer) StartAppJob(app, typ, service string) ([]*discoverd.Instance, error) {
	f.l.Info(fmt.Sprintf("no %s %s process running, getting release details from hosts", app, typ))
	releases := f.FindAppReleaseJobs(app, typ)
	if len(releases) == 0 {
		return nil, fmt.Errorf("didn't find any %s %s release jobs", app, typ)
	}

	// get a job template from the first release
	var job *host.Job
	for _, job = range releases[0] {
		break
	}
	job.ID = cluster.GenerateJobID(f.hosts[0].ID(), "")
	f.FixJobEnv(job)
	// run it on a host
	f.l.Info(fmt.Sprintf("starting %s %s job", app, typ), "job.id", job.ID, "release", job.Metadata["flynn-controller.release"])
	if err := f.hosts[0].AddJob(job); err != nil {
		return nil, fmt.Errorf("error starting %s %s job: %s", app, typ, err)
	}
	f.l.Info("waiting for job to start")
	return discoverd.GetInstances(service, time.Minute)
}

func (f *ClusterFixer) FixDiscoverd() error {
	f.l.Info("ensuring discoverd is running on all hosts")
	releases := f.FindAppReleaseJobs("discoverd", "app")
	if len(releases) == 0 {
		return fmt.Errorf("didn't find any discoverd release jobs")
	}
outer:
	for hostID, job := range releases[0] {
		for _, h := range f.hosts {
			if h.ID() != hostID {
				continue
			}

			// check if discoverd is already running on this host
			jobs, err := h.ListJobs()
			if err != nil {
				return fmt.Errorf("error listing jobs on %s: %s", h.ID(), err)
			}
			for _, j := range jobs {
				if j.Status == host.StatusRunning &&
					j.Job.Metadata["flynn-controller.app_name"] == "discoverd" &&
					j.Job.Metadata["flynn-controller.type"] == "app" {
					continue outer
				}
			}

			job.ID = cluster.GenerateJobID(h.ID(), "")
			f.FixJobEnv(job)
			if err := h.AddJob(job); err != nil {
				return fmt.Errorf("error starting discoverd on %s: %s", h.ID(), err)
			}
			f.l.Info("started discoverd instance", "job.id", job.ID)
			break
		}
	}
	return nil
}

func (f *ClusterFixer) FixFlannel() error {
	f.l.Info("checking flannel")

	flannelJobs := make(map[string]*host.Job, len(f.hosts))
	for _, h := range f.hosts {
		jobs, err := h.ListJobs()
		if err != nil {
			fmt.Errorf("error getting jobs list from %s: %s", h.ID(), err)
		}
		for _, j := range jobs {
			if j.Status != host.StatusRunning ||
				j.Job.Metadata["flynn-controller.app_name"] != "flannel" ||
				j.Job.Metadata["flynn-controller.type"] != "app" {
				continue
			}
			flannelJobs[h.ID()] = j.Job
			break
		}
	}
	if len(flannelJobs) == len(f.hosts) {
		f.l.Info("flannel looks good")
		return nil
	}

	var job *host.Job
	if len(flannelJobs) == 0 {
		f.l.Info("flannel not running, starting it on each host")
		releases := f.FindAppReleaseJobs("flannel", "app")
		if len(releases) == 0 {
			return fmt.Errorf("didn't find flannel release jobs")
		}
		for _, j := range releases[0] {
			job = j
			break
		}
	} else {
		f.l.Info("flannel is not running on each host, starting missing jobs")
		for _, job = range flannelJobs {
			break
		}
	}

	for _, h := range f.hosts {
		if _, ok := flannelJobs[h.ID()]; ok {
			continue
		}
		job.ID = cluster.GenerateJobID(h.ID(), "")
		f.FixJobEnv(job)
		if err := h.AddJob(job); err != nil {
			return fmt.Errorf("error starting flannel job: %s", err)
		}
		f.l.Info("started flannel job", "job.id", job.ID)
	}

	f.l.Info("flannel fix complete")

	return nil
}

func (f *ClusterFixer) FixController(instances []*discoverd.Instance, startScheduler bool) error {
	f.l.Info("found controller instance, checking critical formations")
	inst := instances[0]
	client, err := controller.NewClient("http://"+inst.Addr, inst.Meta["AUTH_KEY"])
	if err != nil {
		return fmt.Errorf("unexpected error creating controller client: %s", err)
	}

	// check that formations for critical components are expected
	apps := []string{"controller", "router", "discoverd", "flannel", "postgres"}
	changes := make(map[string]*ct.Formation, len(apps))
	var controllerFormation *ct.Formation
	for _, app := range apps {
		release, err := client.GetAppRelease(app)
		if err != nil {
			return fmt.Errorf("error getting %s release: %s", app, err)
		}
		formation, err := client.GetFormation(app, release.ID)
		if err != nil {
			// TODO: handle ErrNotFound
			return fmt.Errorf("error getting %s formation: %s", app, err)
		}
		if app == "controller" {
			controllerFormation = formation
		}
		for typ := range release.Processes {
			var want int
			if app == "postgres" && typ == "postgres" && len(f.hosts) > 1 && formation.Processes[typ] < 3 {
				want = 3
			} else if formation.Processes[typ] < 1 {
				want = 1
			}
			if want > 0 {
				f.l.Info("found broken formation", "app", app, "process", typ)
				if _, ok := changes[app]; !ok {
					if formation.Processes == nil {
						formation.Processes = make(map[string]int)
					}
					changes[app] = formation
				}
				changes[app].Processes[typ] = want
			}
		}
	}

	for app, formation := range changes {
		f.l.Info("fixing broken formation", "app", app)
		if err := client.PutFormation(formation); err != nil {
			return fmt.Errorf("error putting %s formation: %s", app, err)
		}
	}

	if startScheduler {
		if err := f.StartScheduler(client, controllerFormation); err != nil {
			return err
		}
	}
	return nil
}

func (f *ClusterFixer) StartScheduler(client *controller.Client, cf *ct.Formation) error {
	if _, err := discoverd.NewService("controller-scheduler").Leader(); err != nil && !discoverd.IsNotFound(err) {
		return fmt.Errorf("error getting scheduler leader: %s", err)
	} else if err == nil {
		f.l.Info("scheduler looks up, moving on")
		return nil
	}
	f.l.Info("scheduler is not up, attempting to fix")

	// start scheduler
	ef, err := utils.ExpandFormation(client, cf)
	if err != nil {
		return err
	}
	schedulerJob := utils.JobConfig(ef, "scheduler", f.hosts[0].ID(), "")
	if err := f.hosts[0].AddJob(schedulerJob); err != nil {
		return fmt.Errorf("error starting scheduler job on %s: %s", f.hosts[0].ID(), err)
	}
	f.l.Info("started scheduler job")
	return nil
}

func (f *ClusterFixer) KillSchedulers() error {
	f.l.Info("killing any running schedulers to prevent interference")
	for _, h := range f.hosts {
		jobs, err := h.ListJobs()
		if err != nil {
			return fmt.Errorf("error listing jobs from %s: %s", h.ID(), err)
		}
		for _, j := range jobs {
			if j.Job.Metadata["flynn-controller.app_name"] != "controller" || j.Job.Metadata["flynn-controller.type"] != "scheduler" {
				continue
			}
			if j.Status != host.StatusRunning && j.Status != host.StatusStarting {
				continue
			}
			if err := h.StopJob(j.Job.ID); err != nil {
				f.l.Error("error stopping scheduler job", "id", j.Job.ID, "error", err)
			}
			f.l.Info("stopped scheduler instance", "job.id", j.Job.ID)
		}
	}
	return nil
}

func (f *ClusterFixer) FixPostgres() error {
	f.l.Info("checking postgres")
	service := discoverd.NewService("postgres")
	leader, _ := service.Leader()
	if leader == nil || leader.Addr == "" {
		f.l.Info("no running postgres leader")
		leader = nil
	} else {
		f.l.Info("found running postgres leader")
	}
	instances, _ := service.Instances()
	f.l.Info(fmt.Sprintf("found %d running postgres instances", len(instances)))

	f.l.Info("getting postgres status")
	var status *client.Status
	if leader != nil && leader.Addr != "" {
		client := client.NewClient(leader.Addr)
		var err error
		status, err = client.Status()
		if err != nil {
			f.l.Error("error getting status from postgres leader", "error", err)
		}
	}
	if status != nil && status.Database.ReadWrite {
		f.l.Info("postgres claims to be read-write")
		return nil
	}

	f.l.Info("getting postgres service metadata")
	meta, err := discoverd.NewService("postgres").GetMeta()
	if err != nil {
		return fmt.Errorf("error getting postgres state from discoverd: %s", err)
	}

	var state pgstate.State
	if err := json.Unmarshal(meta.Data, &state); err != nil {
		return fmt.Errorf("error decoding postgres state: %s", err)
	}
	if state.Primary == nil {
		return fmt.Errorf("no primary in postgres state")
	}

	f.l.Info("getting postgres primary job info", "job.id", state.Primary.Meta["FLYNN_JOB_ID"])
	job, host, err := f.GetJob(state.Primary.Meta["FLYNN_JOB_ID"])
	if err != nil {
		if state.Sync != nil {
			f.l.Error("unable to get primary job info", "error", err)
			f.l.Info("getting postgres sync job info", "job.id", state.Sync.Meta["FLYNN_JOB_ID"])
			job, host, err = f.GetJob(state.Sync.Meta["FLYNN_JOB_ID"])
			if err != nil {
				return fmt.Errorf("unable to get postgres primary or sync job details: %s", err)
			}
		} else {
			return fmt.Errorf("unable to get postgres primary job details: %s", err)
		}
	}

	if leader != nil && state.Singleton {
		return fmt.Errorf("postgres leader is running in singleton mode, unable to fix")
	}

	waitForInstance := func(jobID string) (func() (string, error), error) {
		watchCh := make(chan *discoverd.Event)
		upCh := make(chan string)
		stream, err := service.Watch(watchCh)
		if err != nil {
			return nil, fmt.Errorf("error watching discoverd service: %s", err)
		}
		go func() {
			var current bool
			for event := range watchCh {
				if event.Kind == discoverd.EventKindCurrent {
					current = true
					continue
				}
				if !current || event.Kind != discoverd.EventKindUp {
					continue
				}
				if event.Instance.Meta["FLYNN_JOB_ID"] == jobID {
					upCh <- event.Instance.Addr
				}
			}
		}()
		return func() (string, error) {
			f.l.Info("waiting for postgres instance to start", "job.id", jobID)
			defer stream.Close()
			select {
			case addr := <-upCh:
				return addr, nil
			case <-time.After(time.Minute):
				return "", fmt.Errorf("timed out waiting for postgres instance to come up")
			}
		}, nil
	}

	var wait func() (string, error)
	have := len(instances)
	want := 2
	if state.Singleton {
		want = 1
	}
	if have >= want {
		return fmt.Errorf("already have enough postgres instances, unable to fix")
	}
	f.l.Info("attempting to start missing postgres jobs", "want", want, "have", have)
	if leader == nil {
		// if no postgres, attempt to start
		job.ID = cluster.GenerateJobID(host.ID(), "")
		f.FixJobEnv(job)
		f.l.Info("starting postgres primary job", "job.id", job.ID)
		wait, err = waitForInstance(job.ID)
		if err != nil {
			return err
		}
		if err := host.AddJob(job); err != nil {
			return fmt.Errorf("error starting postgres primary job on %s: %s", host.ID(), err)
		}
		have++
	}
	if want > have {
		// if not enough postgres instances, start another
		var secondHost *cluster.Host
		for _, h := range f.hosts {
			if h.ID() != host.ID() {
				secondHost = h
				break
			}
		}
		if secondHost == nil {
			// if there are no other hosts, use the same one we put the primary on
			secondHost = host
		}
		job.ID = cluster.GenerateJobID(secondHost.ID(), "")
		f.FixJobEnv(job)
		f.l.Info("starting second postgres job", "job.id", job.ID)
		if wait == nil {
			wait, err = waitForInstance(job.ID)
			if err != nil {
				return err
			}
		}
		if err := utils.ProvisionVolume(secondHost, job); err != nil {
			return fmt.Errorf("error creating postgres volume on %s: %s", secondHost.ID(), err)
		}
		if err := secondHost.AddJob(job); err != nil {
			return fmt.Errorf("error starting additional postgres job on %s: %s", secondHost.ID(), err)
		}
	}

	if wait != nil {
		addr, err := wait()
		if err != nil {
			return err
		}
		if leader != nil {
			addr = leader.Addr
		}
		f.l.Info("waiting for postgres to come up read-write")
		return client.NewClient(addr).WaitForReadWrite(5 * time.Minute)
	}
	return nil
}

func (f *ClusterFixer) GetJob(jobID string) (*host.Job, *cluster.Host, error) {
	hostID, err := cluster.ExtractHostID(jobID)
	if err != nil {
		return nil, nil, fmt.Errorf("error parsing host ID from %q", jobID)
	}
	host, err := f.c.Host(hostID)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to get host for job lookup: %s", err)
	}
	job, err := host.GetJob(jobID)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to get job from host: %s", err)
	}
	return job.Job, host, nil
}

// FindAppReleaseJobs returns a slice with one map of host id to job for each
// known release of the given app and type, most recent first
func (f *ClusterFixer) FindAppReleaseJobs(app, typ string) []map[string]*host.Job {
	var sortReleases ReleasesByCreate
	releases := make(map[string]map[string]*host.ActiveJob) // map of releaseID -> hostID -> job ordered
	// connect to each host, list jobs, find distinct releases
	for _, h := range f.hosts {
		jobs, err := h.ListJobs()
		if err != nil {
			f.l.Error("error listing jobs", "host", h.ID(), "error", err)
			continue
		}
		for _, j := range jobs {
			if j.Job.Metadata["flynn-controller.app_name"] != app || j.Job.Metadata["flynn-controller.type"] != typ {
				continue
			}
			id := j.Job.Metadata["flynn-controller.release"]
			if id == "" {
				continue
			}
			m, ok := releases[id]
			if !ok {
				sortReleases = append(sortReleases, SortableRelease{id, j.StartedAt})
				m = make(map[string]*host.ActiveJob)
				releases[id] = m
			}
			if curr, ok := m[h.ID()]; ok && curr.StartedAt.Before(j.StartedAt) {
				continue
			}
			jobCopy := j
			m[h.ID()] = &jobCopy
		}
	}
	sort.Sort(sortReleases)
	res := make([]map[string]*host.Job, len(sortReleases))
	for i, r := range sortReleases {
		res[i] = make(map[string]*host.Job, len(releases[r.id]))
		for k, v := range releases[r.id] {
			res[i][k] = v.Job
		}
	}
	return res
}

func (f *ClusterFixer) FixJobEnv(job *host.Job) {
	job.Config.Env["FLYNN_JOB_ID"] = job.ID
}

type SortableRelease struct {
	id string
	ts time.Time
}

type ReleasesByCreate []SortableRelease

func (p ReleasesByCreate) Len() int           { return len(p) }
func (p ReleasesByCreate) Less(i, j int) bool { return p[i].ts.After(p[j].ts) }
func (p ReleasesByCreate) Swap(i, j int)      { p[i], p[j] = p[j], p[i] }
