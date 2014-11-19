package main

import (
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	c "github.com/flynn/flynn/Godeps/_workspace/src/gopkg.in/check.v1"
	"github.com/flynn/flynn/cli/config"
	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/pkg/attempt"
	"github.com/flynn/flynn/pkg/cluster"
	"github.com/flynn/flynn/pkg/random"
)

type SchedulerSuite struct {
	config        *config.Cluster
	cluster       *cluster.Client
	controller    *controller.Client
	disc          *discoverd.Client
	hosts         map[string]cluster.Host
	slugrunnerURI string
}

var _ = c.Suite(&SchedulerSuite{})

func newControllerClient(t *c.C, cluster *config.Cluster) *controller.Client {
	pin, err := base64.StdEncoding.DecodeString(cluster.TLSPin)
	t.Assert(err, c.IsNil)
	client, err := controller.NewClientWithPin(cluster.URL, cluster.Key, pin)
	t.Assert(err, c.IsNil)

	return client
}

func (s *SchedulerSuite) SetUpSuite(t *c.C) {
	conf, err := config.ReadFile(flynnrc)
	t.Assert(err, c.IsNil)
	t.Assert(conf.Clusters, c.HasLen, 1)
	s.config = conf.Clusters[0]

	s.controller = newControllerClient(t, s.config)

	r, err := s.controller.GetAppRelease("gitreceive")
	t.Assert(err, c.IsNil)
	s.slugrunnerURI = r.Processes["app"].Env["SLUGRUNNER_IMAGE_URI"]
	t.Assert(s.slugrunnerURI, c.Not(c.Equals), "")

	s.disc, err = discoverd.NewClientWithAddr(routerIP + ":1111")
	t.Assert(err, c.IsNil)
}

func tryClose(clients ...io.Closer) {
	for _, client := range clients {
		if client != nil {
			client.Close()
		}
	}
}

func (s *SchedulerSuite) TearDownSuite(t *c.C) {
	tryClose(s.disc, s.cluster, s.controller)
	for _, h := range s.hosts {
		h.Close()
	}
}

func (s *SchedulerSuite) clusterClient(t *c.C) *cluster.Client {
	if s.cluster == nil {
		var err error
		s.cluster, err = cluster.NewClientWithDial(nil, s.disc.NewServiceSet)
		t.Assert(err, c.IsNil)
	}
	return s.cluster
}

func (s *SchedulerSuite) hostClient(t *c.C, hostID string) cluster.Host {
	if s.hosts == nil {
		s.hosts = make(map[string]cluster.Host)
	}
	if client, ok := s.hosts[hostID]; ok {
		return client
	}
	client, err := s.clusterClient(t).DialHost(hostID)
	t.Assert(err, c.IsNil)
	s.hosts[hostID] = client
	return client
}

func (s *SchedulerSuite) stopJob(t *c.C, id string) {
	debug("stopping job", id)
	hostID, jobID, _ := cluster.ParseJobID(id)
	hc := s.hostClient(t, hostID)
	t.Assert(hc.StopJob(jobID), c.IsNil)
}

func (s *SchedulerSuite) checkJobState(t *c.C, appID, jobID, state string) {
	job, err := s.controller.GetJob(appID, jobID)
	t.Assert(err, c.IsNil)
	t.Assert(job.State, c.Equals, state)
}

func (s *SchedulerSuite) createApp(t *c.C) (*ct.App, *ct.Release) {
	app := &ct.App{}
	t.Assert(s.controller.CreateApp(app), c.IsNil)
	debugf("created app %s (%s)", app.Name, app.ID)

	artifact := &ct.Artifact{Type: "docker", URI: s.slugrunnerURI}
	t.Assert(s.controller.CreateArtifact(artifact), c.IsNil)

	release := &ct.Release{
		ArtifactID: artifact.ID,
		Processes: map[string]ct.ProcessType{
			"echoer": {
				Entrypoint: []string{"bash", "-c"},
				Cmd:        []string{"sdutil exec -s echo-service:$PORT socat -v tcp-l:$PORT,fork exec:/bin/cat"},
				Ports:      []ct.Port{{Proto: "tcp"}},
			},
			"printer": {
				Entrypoint: []string{"bash", "-c"},
				Cmd:        []string{"while true; do echo I like to print; sleep 1; done"},
				Ports:      []ct.Port{{Proto: "tcp"}},
			},
			"crasher": {
				Entrypoint: []string{"bash", "-c"},
				Cmd:        []string{"trap 'exit 1' SIGTERM; while true; do echo I like to crash; sleep 1; done"},
			},
			"omni": {
				Entrypoint: []string{"bash", "-c"},
				Cmd:        []string{"while true; do echo I am everywhere; sleep 1; done"},
				Omni:       true,
			},
		},
	}
	t.Assert(s.controller.CreateRelease(release), c.IsNil)
	t.Assert(s.controller.SetAppRelease(app.ID, release.ID), c.IsNil)
	return app, release
}

func (s *SchedulerSuite) addHosts(t *c.C, count int) []string {
	debugf("adding %d hosts", count)
	ch := make(chan *host.HostEvent)
	stream := s.clusterClient(t).StreamHostEvents(ch)
	defer stream.Close()

	hosts := make([]string, 0, count)
	for i := 0; i < count; i++ {
		res, err := httpClient.PostForm(args.ClusterAPI, url.Values{})
		if err != nil {
			t.Fatal("error in POST request to cluster api:", err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatal("expected 200 status, got", res.Status)
		}

		select {
		case event := <-ch:
			debug("host added", event.HostID)
			hosts = append(hosts, event.HostID)
		case <-time.After(20 * time.Second):
			t.Fatal("timed out waiting for new host")
		}
	}
	return hosts
}

func (s *SchedulerSuite) removeHosts(t *c.C, ids []string) {
	debugf("removing %d hosts", len(ids))

	// Wait for router-api services to disappear to indicate host
	// removal (rather than using StreamHostEvents), so that other
	// tests won't try and connect to this host via service discovery.
	set, err := s.disc.NewServiceSet("router-api")
	t.Assert(err, c.IsNil)
	defer set.Close()
	updates := set.Watch(false)
	defer set.Unwatch(updates)

	for _, id := range ids {
		req, err := http.NewRequest("DELETE", args.ClusterAPI+"?host="+id, nil)
		if err != nil {
			t.Fatal("error in DELETE request to cluster api:", err)
		}
		res, err := httpClient.Do(req)
		if err != nil {
			t.Fatal("error in DELETE request to cluster api:", err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatal("expected 200 status, got", res.Status)
		}

	loop:
		for {
			select {
			case update := <-updates:
				if !update.Online {
					debug("host removed", update.Addr)
					break loop
				}
			case <-time.After(20 * time.Second):
				t.Fatal("timed out waiting for host removal")
			}
		}
	}
}

func processesEqual(expected, actual map[string]int) bool {
	for t, n := range expected {
		if actual[t] != n {
			return false
		}
	}
	return true
}

func waitForJobEvents(t *c.C, events chan *ct.JobEvent, diff map[string]int) (lastID int64) {
	debugf("waiting for job events: %v", diff)
	actual := make(map[string]int)
	for {
	inner:
		select {
		case event := <-events:
			debug("got job event:", event.Type, event.JobID, event.State)
			lastID = event.ID
			switch event.State {
			case "up":
				actual[event.Type] += 1
			case "down", "crashed":
				actual[event.Type] -= 1
			default:
				break inner
			}
			if processesEqual(diff, actual) {
				return
			}
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for job events: ", diff)
		}
	}
}

func waitForJobRestart(t *c.C, events chan *ct.JobEvent, timeout time.Duration) string {
	debug("waiting for job restart")
	for {
		select {
		case event := <-events:
			debug("got job event:", event.Type, event.JobID, event.State)
			if event.State == "up" {
				return event.JobID
			}
		case <-time.After(timeout):
			t.Fatal("timed out waiting for job restart")
		}
	}
}

func (s *SchedulerSuite) TestScale(t *c.C) {
	app, release := s.createApp(t)

	stream, err := s.controller.StreamJobEvents(app.ID, 0)
	t.Assert(err, c.IsNil)
	defer stream.Close()

	formation := &ct.Formation{
		AppID:     app.ID,
		ReleaseID: release.ID,
		Processes: make(map[string]int),
	}

	current := make(map[string]int)
	updates := []map[string]int{
		{"printer": 2},
		{"printer": 3, "crasher": 1},
		{"printer": 1},
	}

	for _, procs := range updates {
		debugf("scaling formation to %v", procs)
		formation.Processes = procs
		t.Assert(s.controller.PutFormation(formation), c.IsNil)

		diff := make(map[string]int)
		for t, n := range procs {
			diff[t] = n - current[t]
		}
		for t, n := range current {
			if _, ok := procs[t]; !ok {
				diff[t] = -n
			}
		}
		waitForJobEvents(t, stream.Events, diff)

		current = procs
	}
}

func (s *SchedulerSuite) TestControllerRestart(t *c.C) {
	// get the current controller details
	app, err := s.controller.GetApp("controller")
	t.Assert(err, c.IsNil)
	release, err := s.controller.GetAppRelease("controller")
	t.Assert(err, c.IsNil)
	list, err := s.controller.JobList("controller")
	t.Assert(err, c.IsNil)
	var jobs []*ct.Job
	for _, job := range list {
		if job.Type == "web" {
			jobs = append(jobs, job)
		}
	}
	t.Assert(jobs, c.HasLen, 1)
	hostID, jobID, _ := cluster.ParseJobID(jobs[0].ID)
	t.Assert(hostID, c.Not(c.Equals), "")
	t.Assert(jobID, c.Not(c.Equals), "")
	debugf("current controller app[%s] host[%s] job[%s]", app.ID, hostID, jobID)

	// start a second controller and wait for it to come up
	stream, err := s.controller.StreamJobEvents("controller", 0)
	t.Assert(err, c.IsNil)
	debug("scaling the controller up")
	t.Assert(s.controller.PutFormation(&ct.Formation{
		AppID:     app.ID,
		ReleaseID: release.ID,
		Processes: map[string]int{"web": 2, "scheduler": 1},
	}), c.IsNil)
	lastID := waitForJobEvents(t, stream.Events, map[string]int{"web": 1})
	stream.Close()

	// get direct client for new controller
	var client *controller.Client
	attempts := attempt.Strategy{
		Total: 10 * time.Second,
		Delay: 500 * time.Millisecond,
	}
	t.Assert(attempts.Run(func() (err error) {
		set, err := s.disc.NewServiceSet("flynn-controller")
		if err != nil {
			return err
		}
		defer set.Close()
		addrs := set.Addrs()
		if len(addrs) != 2 {
			return fmt.Errorf("expected 2 controller processes, got %d", len(addrs))
		}
		addr := addrs[1]
		debug("new controller address:", addr)
		client, err = controller.NewClient("http://"+addr, s.config.Key)
		return
	}), c.IsNil)

	// kill the first controller and check the scheduler brings it back online
	stream, err = client.StreamJobEvents("controller", lastID)
	defer stream.Close()
	t.Assert(err, c.IsNil)
	cc, err := cluster.NewClientWithDial(nil, s.disc.NewServiceSet)
	t.Assert(err, c.IsNil)
	defer cc.Close()
	hc, err := cc.DialHost(hostID)
	t.Assert(err, c.IsNil)
	defer hc.Close()
	debug("stopping job", jobID)
	t.Assert(hc.StopJob(jobID), c.IsNil)
	waitForJobEvents(t, stream.Events, map[string]int{"web": 0})

	// scale back down
	debug("scaling the controller down")
	t.Assert(s.controller.PutFormation(&ct.Formation{
		AppID:     app.ID,
		ReleaseID: release.ID,
		Processes: map[string]int{"web": 1, "scheduler": 1},
	}), c.IsNil)
	waitForJobEvents(t, stream.Events, map[string]int{"web": -1})

	// set the suite's client to the new controller for other tests
	s.controller = client
}

func (s *SchedulerSuite) TestJobStatus(t *c.C) {
	app, release := s.createApp(t)

	stream, err := s.controller.StreamJobEvents(app.ID, 0)
	t.Assert(err, c.IsNil)
	defer stream.Close()

	// start 2 formation processes and 1 one-off job
	t.Assert(s.controller.PutFormation(&ct.Formation{
		AppID:     app.ID,
		ReleaseID: release.ID,
		Processes: map[string]int{"printer": 1, "crasher": 1},
	}), c.IsNil)
	_, err = s.controller.RunJobDetached(app.ID, &ct.NewJob{
		ReleaseID:  release.ID,
		Entrypoint: []string{"bash", "-c"},
		Cmd:        []string{"while true; do echo one-off-job; sleep 1; done"},
	})
	t.Assert(err, c.IsNil)
	waitForJobEvents(t, stream.Events, map[string]int{"printer": 1, "crasher": 1, "": 1})

	list, err := s.controller.JobList(app.ID)
	t.Assert(err, c.IsNil)
	t.Assert(list, c.HasLen, 3)
	jobs := make(map[string]*ct.Job, len(list))
	for _, job := range list {
		debug(job.Type, "job started with ID", job.ID)
		jobs[job.Type] = job
	}

	// Check jobs are marked as up once started
	t.Assert(jobs["printer"].State, c.Equals, "up")
	t.Assert(jobs["crasher"].State, c.Equals, "up")
	t.Assert(jobs[""].State, c.Equals, "up")

	// Check that when a formation's job is removed, it is marked as down and a new one is scheduled
	job := jobs["printer"]
	s.stopJob(t, job.ID)
	waitForJobEvents(t, stream.Events, map[string]int{"printer": 0})
	s.checkJobState(t, app.ID, job.ID, "down")
	list, err = s.controller.JobList(app.ID)
	t.Assert(err, c.IsNil)
	t.Assert(list, c.HasLen, 4)

	// Check that when a one-off job is removed, it is marked as down but a new one is not scheduled
	job = jobs[""]
	s.stopJob(t, job.ID)
	waitForJobEvents(t, stream.Events, map[string]int{"": -1})
	s.checkJobState(t, app.ID, job.ID, "down")
	list, err = s.controller.JobList(app.ID)
	t.Assert(err, c.IsNil)
	t.Assert(list, c.HasLen, 4)

	// Check that when a job errors, it is marked as crashed and a new one is started
	job = jobs["crasher"]
	s.stopJob(t, job.ID)
	waitForJobEvents(t, stream.Events, map[string]int{"crasher": 0})
	s.checkJobState(t, app.ID, job.ID, "crashed")
	list, err = s.controller.JobList(app.ID)
	t.Assert(err, c.IsNil)
	t.Assert(list, c.HasLen, 5)
}

func (s *SchedulerSuite) TestOmniJobs(t *c.C) {
	if args.ClusterAPI == "" {
		t.Skip("cannot boot new hosts")
	}

	app, release := s.createApp(t)

	stream, err := s.controller.StreamJobEvents(app.ID, 0)
	t.Assert(err, c.IsNil)
	defer stream.Close()

	formation := &ct.Formation{
		AppID:     app.ID,
		ReleaseID: release.ID,
		Processes: make(map[string]int),
	}

	current := make(map[string]int)
	updates := []map[string]int{
		{"printer": 2},
		{"printer": 3, "omni": 2},
		{"printer": 1, "omni": 1},
	}

	for _, procs := range updates {
		debugf("scaling formation to %v", procs)
		formation.Processes = procs
		t.Assert(s.controller.PutFormation(formation), c.IsNil)

		diff := make(map[string]int)
		for t, n := range procs {
			diff[t] = n - current[t]
			if t == "omni" {
				diff[t] *= testCluster.Size()
			}
		}
		for t, n := range current {
			if _, ok := procs[t]; !ok {
				diff[t] = -n
				if t == "omni" {
					diff[t] *= testCluster.Size()
				}
			}
		}
		waitForJobEvents(t, stream.Events, diff)

		current = procs
	}

	// Check that new hosts get omni jobs
	newHosts := s.addHosts(t, 2)
	defer s.removeHosts(t, newHosts)
	waitForJobEvents(t, stream.Events, map[string]int{"omni": 2})
}

func (s *SchedulerSuite) TestJobRestartBackoffPolicy(t *c.C) {
	if testCluster == nil {
		t.Skip("cannot determine scheduler backoff period")
	}
	backoffPeriod := testCluster.BackoffPeriod
	startTimeout := 20 * time.Second
	debugf("job restart backoff period: %s", backoffPeriod)

	app, release := s.createApp(t)

	stream, err := s.controller.StreamJobEvents(app.ID, 0)
	t.Assert(err, c.IsNil)
	defer stream.Close()

	t.Assert(s.controller.PutFormation(&ct.Formation{
		AppID:     app.ID,
		ReleaseID: release.ID,
		Processes: map[string]int{"printer": 1},
	}), c.IsNil)
	id := waitForJobRestart(t, stream.Events, startTimeout)

	// First restart: scheduled immediately
	s.stopJob(t, id)
	id = waitForJobRestart(t, stream.Events, startTimeout)

	// Second restart after 1 * backoffPeriod
	start := time.Now()
	s.stopJob(t, id)
	id = waitForJobRestart(t, stream.Events, backoffPeriod+startTimeout)
	t.Assert(time.Now().Sub(start) > backoffPeriod, c.Equals, true)

	// Third restart after 2 * backoffPeriod
	start = time.Now()
	s.stopJob(t, id)
	id = waitForJobRestart(t, stream.Events, 2*backoffPeriod+startTimeout)
	t.Assert(time.Now().Sub(start) > 2*backoffPeriod, c.Equals, true)

	// After backoffPeriod has elapsed: scheduled immediately
	time.Sleep(backoffPeriod)
	s.stopJob(t, id)
	waitForJobRestart(t, stream.Events, startTimeout)
}

func (s *SchedulerSuite) TestTCPApp(t *c.C) {
	app, _ := s.createApp(t)

	stream, err := s.controller.StreamJobEvents(app.ID, 0)
	t.Assert(err, c.IsNil)
	defer stream.Close()

	t.Assert(flynn("/", "-a", app.Name, "scale", "echoer=1"), Succeeds)

	newRoute := flynn("/", "-a", app.Name, "route", "add", "tcp", "-s", "echo-service")
	t.Assert(newRoute, Succeeds)
	t.Assert(newRoute.Output, Matches, `.+ on port \d+`)
	str := strings.Split(strings.TrimSpace(string(newRoute.Output)), " ")
	port := str[len(str)-1]

	waitForJobEvents(t, stream.Events, map[string]int{"echoer": 1})
	// use Attempts to give the processes time to start
	if err := Attempts.Run(func() error {
		servAddr := routerIP + ":" + port
		conn, err := net.Dial("tcp", servAddr)
		if err != nil {
			return err
		}
		defer conn.Close()
		echo := random.Bytes(16)
		_, err = conn.Write(echo)
		if err != nil {
			return err
		}
		reply := make([]byte, 16)
		_, err = conn.Read(reply)
		if err != nil {
			return err
		}
		t.Assert(reply, c.DeepEquals, echo)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
