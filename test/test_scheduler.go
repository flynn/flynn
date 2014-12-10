package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	c "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/pkg/attempt"
	"github.com/flynn/flynn/pkg/cluster"
	"github.com/flynn/flynn/pkg/stream"
	tc "github.com/flynn/flynn/test/cluster"
)

type SchedulerSuite struct {
	Helper
}

var _ = c.Suite(&SchedulerSuite{})

func (s *SchedulerSuite) checkJobState(t *c.C, appID, jobID, state string) {
	job, err := s.controllerClient(t).GetJob(appID, jobID)
	t.Assert(err, c.IsNil)
	t.Assert(job.State, c.Equals, state)
}

func (s *SchedulerSuite) addHosts(t *c.C, count int) []string {
	debugf(t, "adding %d hosts", count)
	ch := make(chan *host.HostEvent)
	stream, err := s.clusterClient(t).StreamHostEvents(ch)
	if err != nil {
		t.Fatal("error when attempting to StreamHostEvents", err)
	}
	defer stream.Close()

	hosts := make([]string, 0, count)
	for i := 0; i < count; i++ {
		res, err := httpClient.PostForm(args.ClusterAPI, url.Values{})
		if err != nil {
			t.Fatal("error in POST request to cluster api:", err)
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatal("expected 200 status, got", res.Status)
		}
		instance := &tc.Instance{}
		err = json.NewDecoder(res.Body).Decode(instance)
		if err != nil {
			t.Fatal("could not decode new instance:", err)
		}

		select {
		case event := <-ch:
			debug(t, "host added ", event.HostID)
			testCluster.Instances = append(testCluster.Instances, instance)
			hosts = append(hosts, event.HostID)
		case <-time.After(20 * time.Second):
			t.Fatal("timed out waiting for new host")
		}
	}
	return hosts
}

func (s *SchedulerSuite) removeHosts(t *c.C, ids []string) {
	debugf(t, "removing %d hosts", len(ids))

	// Wait for router-api services to disappear to indicate host
	// removal (rather than using StreamHostEvents), so that other
	// tests won't try and connect to this host via service discovery.
	set, err := s.discoverdClient(t).NewServiceSet("router-api")
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
					debug(t, "host removed ", update.Addr)
					break loop
				}
			case <-time.After(20 * time.Second):
				t.Fatal("timed out waiting for host removal")
			}
		}
	}
}

func jobEventsEqual(expected, actual jobEvents) bool {
	for typ, events := range expected {
		diff, ok := actual[typ]
		if !ok {
			return false
		}
		for state, count := range events {
			if diff[state] != count {
				return false
			}
		}
	}
	return true
}

type jobEvents map[string]map[string]int

func waitForJobEvents(t *c.C, stream stream.Stream, events chan *ct.JobEvent, expected jobEvents) (lastID int64, jobID string) {
	debugf(t, "waiting for job events: %v", expected)
	actual := make(jobEvents)
	for {
	inner:
		select {
		case event, ok := <-events:
			if !ok {
				t.Fatal("job event stream closed: %s", stream.Err())
			}
			debugf(t, "got job event: %s %s %s", event.Type, event.JobID, event.State)
			lastID = event.ID
			jobID = event.JobID
			if _, ok := actual[event.Type]; !ok {
				actual[event.Type] = make(map[string]int)
			}
			switch event.State {
			case "up":
				actual[event.Type]["up"] += 1
			case "down", "crashed":
				actual[event.Type]["down"] += 1
			default:
				break inner
			}
			if jobEventsEqual(expected, actual) {
				return
			}
		case <-time.After(60 * time.Second):
			t.Fatal("timed out waiting for job events: ", expected)
		}
	}
}

func waitForJobRestart(t *c.C, stream stream.Stream, events chan *ct.JobEvent, typ string, timeout time.Duration) string {
	debug(t, "waiting for job restart")
	for {
		select {
		case event, ok := <-events:
			if !ok {
				t.Fatal("job event stream closed: %s", stream.Err())
			}
			debug(t, "got job event: ", event.Type, event.JobID, event.State)
			if event.Type == typ && event.State == "up" {
				return event.JobID
			}
		case <-time.After(timeout):
			t.Fatal("timed out waiting for job restart")
		}
	}
}

func (s *SchedulerSuite) TestScale(t *c.C) {
	app, release := s.createApp(t)

	events := make(chan *ct.JobEvent)
	stream, err := s.controllerClient(t).StreamJobEvents(app.ID, 0, events)
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
		debugf(t, "scaling formation to %v", procs)
		formation.Processes = procs
		t.Assert(s.controllerClient(t).PutFormation(formation), c.IsNil)

		expected := make(jobEvents)
		for typ, count := range procs {
			diff := count - current[typ]
			if diff > 0 {
				expected[typ] = map[string]int{"up": diff}
			} else {
				expected[typ] = map[string]int{"down": -diff}
			}
		}
		for typ, count := range current {
			if _, ok := procs[typ]; !ok {
				expected[typ] = map[string]int{"down": count}
			}
		}
		waitForJobEvents(t, stream, events, expected)

		current = procs
	}
}

func (s *SchedulerSuite) TestControllerRestart(t *c.C) {
	// get the current controller details
	app, err := s.controllerClient(t).GetApp("controller")
	t.Assert(err, c.IsNil)
	release, err := s.controllerClient(t).GetAppRelease("controller")
	t.Assert(err, c.IsNil)
	list, err := s.controllerClient(t).JobList("controller")
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
	debugf(t, "current controller app[%s] host[%s] job[%s]", app.ID, hostID, jobID)

	// start a second controller and wait for it to come up
	events := make(chan *ct.JobEvent)
	stream, err := s.controllerClient(t).StreamJobEvents("controller", 0, events)
	t.Assert(err, c.IsNil)
	debug(t, "scaling the controller up")
	t.Assert(s.controllerClient(t).PutFormation(&ct.Formation{
		AppID:     app.ID,
		ReleaseID: release.ID,
		Processes: map[string]int{"web": 2, "scheduler": 1},
	}), c.IsNil)
	lastID, _ := waitForJobEvents(t, stream, events, jobEvents{"web": {"up": 1}})
	stream.Close()

	// get direct client for new controller
	var client *controller.Client
	attempts := attempt.Strategy{
		Total: 10 * time.Second,
		Delay: 500 * time.Millisecond,
	}
	t.Assert(attempts.Run(func() (err error) {
		set, err := s.discoverdClient(t).NewServiceSet("flynn-controller")
		if err != nil {
			return err
		}
		defer set.Close()
		addrs := set.Addrs()
		if len(addrs) != 2 {
			return fmt.Errorf("expected 2 controller processes, got %d", len(addrs))
		}
		addr := addrs[1]
		debug(t, "new controller address: ", addr)
		client, err = controller.NewClient("http://"+addr, s.clusterConf(t).Key)
		if err != nil {
			return err
		}
		events = make(chan *ct.JobEvent)
		stream, err = client.StreamJobEvents("controller", lastID, events)
		return
	}), c.IsNil)
	defer stream.Close()

	// kill the first controller and check the scheduler brings it back online
	cc, err := cluster.NewClientWithServices(s.discoverdClient(t).NewServiceSet)
	t.Assert(err, c.IsNil)
	defer cc.Close()
	hc, err := cc.DialHost(hostID)
	t.Assert(err, c.IsNil)
	defer hc.Close()
	debug(t, "stopping job ", jobID)
	t.Assert(hc.StopJob(jobID), c.IsNil)
	waitForJobEvents(t, stream, events, jobEvents{"web": {"down": 1, "up": 1}})

	// scale back down
	debug(t, "scaling the controller down")
	t.Assert(s.controllerClient(t).PutFormation(&ct.Formation{
		AppID:     app.ID,
		ReleaseID: release.ID,
		Processes: map[string]int{"web": 1, "scheduler": 1},
	}), c.IsNil)
	waitForJobEvents(t, stream, events, jobEvents{"web": {"down": 1}})

	// unset the suite's client so other tests use a new client
	s.controller = nil
}

func (s *SchedulerSuite) TestJobStatus(t *c.C) {
	app, release := s.createApp(t)

	events := make(chan *ct.JobEvent)
	stream, err := s.controllerClient(t).StreamJobEvents(app.ID, 0, events)
	t.Assert(err, c.IsNil)
	defer stream.Close()

	// start 2 formation processes and 1 one-off job
	t.Assert(s.controllerClient(t).PutFormation(&ct.Formation{
		AppID:     app.ID,
		ReleaseID: release.ID,
		Processes: map[string]int{"printer": 1, "crasher": 1},
	}), c.IsNil)
	_, err = s.controllerClient(t).RunJobDetached(app.ID, &ct.NewJob{
		ReleaseID: release.ID,
		Cmd:       []string{"sh", "-c", "while true; do echo one-off-job; sleep 1; done"},
	})
	t.Assert(err, c.IsNil)
	waitForJobEvents(t, stream, events, jobEvents{"printer": {"up": 1}, "crasher": {"up": 1}, "": {"up": 1}})

	list, err := s.controllerClient(t).JobList(app.ID)
	t.Assert(err, c.IsNil)
	t.Assert(list, c.HasLen, 3)
	jobs := make(map[string]*ct.Job, len(list))
	for _, job := range list {
		debug(t, job.Type, "job started with ID ", job.ID)
		jobs[job.Type] = job
	}

	// Check jobs are marked as up once started
	t.Assert(jobs["printer"].State, c.Equals, "up")
	t.Assert(jobs["crasher"].State, c.Equals, "up")
	t.Assert(jobs[""].State, c.Equals, "up")

	// Check that when a formation's job is removed, it is marked as down and a new one is scheduled
	job := jobs["printer"]
	s.stopJob(t, job.ID)
	waitForJobEvents(t, stream, events, jobEvents{"printer": {"down": 1, "up": 1}})
	s.checkJobState(t, app.ID, job.ID, "down")
	list, err = s.controllerClient(t).JobList(app.ID)
	t.Assert(err, c.IsNil)
	t.Assert(list, c.HasLen, 4)

	// Check that when a one-off job is removed, it is marked as down but a new one is not scheduled
	job = jobs[""]
	s.stopJob(t, job.ID)
	waitForJobEvents(t, stream, events, jobEvents{"": {"down": 1}})
	s.checkJobState(t, app.ID, job.ID, "down")
	list, err = s.controllerClient(t).JobList(app.ID)
	t.Assert(err, c.IsNil)
	t.Assert(list, c.HasLen, 4)

	// Check that when a job errors, it is marked as crashed and a new one is started
	job = jobs["crasher"]
	s.stopJob(t, job.ID)
	waitForJobEvents(t, stream, events, jobEvents{"crasher": {"down": 1, "up": 1}})
	s.checkJobState(t, app.ID, job.ID, "crashed")
	list, err = s.controllerClient(t).JobList(app.ID)
	t.Assert(err, c.IsNil)
	t.Assert(list, c.HasLen, 5)
}

func (s *SchedulerSuite) TestOmniJobs(t *c.C) {
	if args.ClusterAPI == "" {
		t.Skip("cannot boot new hosts")
	}

	app, release := s.createApp(t)

	events := make(chan *ct.JobEvent)
	stream, err := s.controllerClient(t).StreamJobEvents(app.ID, 0, events)
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
		debugf(t, "scaling formation to %v", procs)
		formation.Processes = procs
		t.Assert(s.controllerClient(t).PutFormation(formation), c.IsNil)

		expected := make(jobEvents)
		for typ, count := range procs {
			diff := count - current[typ]
			if typ == "omni" {
				diff *= testCluster.Size()
			}
			if diff > 0 {
				expected[typ] = map[string]int{"up": diff}
			} else {
				expected[typ] = map[string]int{"down": -diff}
			}
		}
		for typ, count := range current {
			if _, ok := procs[typ]; !ok {
				diff := count
				if typ == "omni" {
					diff *= testCluster.Size()
				}
				expected[typ] = map[string]int{"down": diff}
			}
		}
		waitForJobEvents(t, stream, events, expected)

		current = procs
	}

	// Check that new hosts get omni jobs
	newHosts := s.addHosts(t, 2)
	defer s.removeHosts(t, newHosts)
	waitForJobEvents(t, stream, events, jobEvents{"omni": {"up": 2}})
}

func (s *SchedulerSuite) TestJobRestartBackoffPolicy(t *c.C) {
	if testCluster == nil {
		t.Skip("cannot determine scheduler backoff period")
	}
	backoffPeriod := testCluster.BackoffPeriod
	startTimeout := 20 * time.Second
	debugf(t, "job restart backoff period: %s", backoffPeriod)

	app, release := s.createApp(t)

	events := make(chan *ct.JobEvent)
	stream, err := s.controllerClient(t).StreamJobEvents(app.ID, 0, events)
	t.Assert(err, c.IsNil)
	defer stream.Close()

	t.Assert(s.controllerClient(t).PutFormation(&ct.Formation{
		AppID:     app.ID,
		ReleaseID: release.ID,
		Processes: map[string]int{"printer": 1},
	}), c.IsNil)
	_, id := waitForJobEvents(t, stream, events, jobEvents{"printer": {"up": 1}})

	// First restart: scheduled immediately
	s.stopJob(t, id)
	id = waitForJobRestart(t, stream, events, "printer", startTimeout)

	// Second restart after 1 * backoffPeriod
	start := time.Now()
	s.stopJob(t, id)
	id = waitForJobRestart(t, stream, events, "printer", backoffPeriod+startTimeout)
	t.Assert(time.Now().Sub(start) > backoffPeriod, c.Equals, true)

	// Third restart after 2 * backoffPeriod
	start = time.Now()
	s.stopJob(t, id)
	id = waitForJobRestart(t, stream, events, "printer", 2*backoffPeriod+startTimeout)
	t.Assert(time.Now().Sub(start) > 2*backoffPeriod, c.Equals, true)

	// After backoffPeriod has elapsed: scheduled immediately
	time.Sleep(backoffPeriod)
	s.stopJob(t, id)
	waitForJobRestart(t, stream, events, "printer", startTimeout)
}

func (s *SchedulerSuite) TestTCPApp(t *c.C) {
	app, _ := s.createApp(t)

	t.Assert(flynn(t, "/", "-a", app.Name, "scale", "echoer=1"), Succeeds)

	newRoute := flynn(t, "/", "-a", app.Name, "route", "add", "tcp", "-s", "echo-service")
	t.Assert(newRoute, Succeeds)
	t.Assert(newRoute.Output, Matches, `.+ on port \d+`)
	str := strings.Split(strings.TrimSpace(string(newRoute.Output)), " ")
	port := str[len(str)-1]

	// use Attempts to give the processes time to start
	if err := Attempts.Run(func() error {
		servAddr := routerIP + ":" + port
		conn, err := net.Dial("tcp", servAddr)
		if err != nil {
			return err
		}
		defer conn.Close()
		msg := []byte("hello there!\n")
		_, err = conn.Write(msg)
		if err != nil {
			return err
		}
		reply := make([]byte, len(msg))
		_, err = conn.Read(reply)
		if err != nil {
			return err
		}
		t.Assert(reply, c.DeepEquals, msg)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
