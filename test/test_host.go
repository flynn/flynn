package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	osexec "os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/controller/utils"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/host/volume"
	logaggc "github.com/flynn/flynn/logaggregator/client"
	logagg "github.com/flynn/flynn/logaggregator/types"
	"github.com/flynn/flynn/pkg/attempt"
	"github.com/flynn/flynn/pkg/cluster"
	"github.com/flynn/flynn/pkg/dialer"
	"github.com/flynn/flynn/pkg/exec"
	hh "github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/random"
	"github.com/flynn/flynn/pkg/schedutil"
	"github.com/flynn/flynn/pkg/stream"
	c "github.com/flynn/go-check"
)

type HostSuite struct {
	Helper
}

var _ = c.ConcurrentSuite(&HostSuite{})

func (s *HostSuite) TestGetNonExistentJob(t *c.C) {
	cluster := s.clusterClient(t)
	hosts, err := cluster.Hosts()
	t.Assert(err, c.IsNil)

	// Getting a non-existent job should error
	_, err = hosts[0].GetJob("i-dont-exist")
	t.Assert(hh.IsObjectNotFoundError(err), c.Equals, true)
}

func (s *HostSuite) TestAddFailingJob(t *c.C) {
	// get a host and watch events
	hosts, err := s.clusterClient(t).Hosts()
	t.Assert(err, c.IsNil)
	t.Assert(hosts, c.Not(c.HasLen), 0)
	h := hosts[0]
	jobID := random.UUID()
	events := make(chan *host.Event)
	stream, err := h.StreamEvents(jobID, events)
	t.Assert(err, c.IsNil)
	defer stream.Close()

	// add a job with a non existent partition
	job := &host.Job{
		ID:         jobID,
		Mountspecs: []*host.Mountspec{{}},
		Partition:  "nonexistent",
	}
	t.Assert(h.AddJob(job), c.IsNil)

	// check we get a create then error event
	actual := make(map[host.JobEventType]*host.Event, 2)
loop:
	for {
		select {
		case e, ok := <-events:
			if !ok {
				t.Fatalf("job event stream closed unexpectedly: %s", stream.Err())
			}
			if _, ok := actual[e.Event]; ok {
				t.Fatalf("unexpected event: %v", e)
			}
			actual[e.Event] = e
			if len(actual) >= 2 {
				break loop
			}
		case <-time.After(30 * time.Second):
			t.Fatal("timed out waiting for job event")
		}
	}
	t.Assert(actual[host.JobEventCreate], c.NotNil)
	e := actual[host.JobEventError]
	t.Assert(e, c.NotNil)
	t.Assert(e.Job, c.NotNil)
	t.Assert(e.Job.Error, c.NotNil)
	t.Assert(*e.Job.Error, c.Equals, `host: invalid job partition "nonexistent"`)
}

func (s *HostSuite) TestAttachNonExistentJob(t *c.C) {
	cluster := s.clusterClient(t)
	hosts, err := cluster.Hosts()
	t.Assert(err, c.IsNil)

	// Attaching to a non-existent job should error
	_, err = hosts[0].Attach(&host.AttachReq{JobID: "none", Flags: host.AttachFlagLogs}, false)
	t.Assert(err, c.NotNil)
}

func (s *HostSuite) TestAttachFinishedInteractiveJob(t *c.C) {
	cluster := s.clusterClient(t)

	// run a quick interactive job
	cmd := exec.CommandUsingCluster(cluster, s.createArtifact(t, "test-apps"), "/bin/true")
	cmd.TTY = true
	runErr := make(chan error)
	go func() {
		runErr <- cmd.Run()
	}()
	select {
	case err := <-runErr:
		t.Assert(err, c.IsNil)
	case <-time.After(30 * time.Second):
		t.Fatal("timed out waiting for interactive job")
	}

	h, err := cluster.Host(cmd.HostID)
	t.Assert(err, c.IsNil)

	// Getting the logs for the job should fail, as it has none because it was
	// interactive
	attachErr := make(chan error)
	go func() {
		_, err = h.Attach(&host.AttachReq{JobID: cmd.Job.ID, Flags: host.AttachFlagLogs}, false)
		attachErr <- err
	}()
	select {
	case err := <-attachErr:
		t.Assert(err, c.NotNil)
	case <-time.After(time.Second):
		t.Error("timed out waiting for attach")
	}
}

func (s *HostSuite) TestExecCrashingJob(t *c.C) {
	cluster := s.clusterClient(t)

	for _, attach := range []bool{true, false} {
		t.Logf("attach = %v", attach)
		cmd := exec.CommandUsingCluster(cluster, s.createArtifact(t, "test-apps"), "sh", "-c", "exit 1")
		if attach {
			cmd.Stdout = ioutil.Discard
			cmd.Stderr = ioutil.Discard
		}
		t.Assert(cmd.Run(), c.DeepEquals, exec.ExitError(1))
	}
}

type IshApp struct {
	t           *c.C
	cmd         *exec.Cmd
	addr        string
	client      controller.Client
	discoverd   *discoverd.Client
	cluster     *Cluster
	proxy       *clusterProxy
	host        *cluster.Host
	app         *ct.App
	extraConfig host.ContainerConfig
}

func (a *IshApp) Cleanup() {
	if a.cmd != nil {
		a.cmd.Kill()
	}
	if a.proxy != nil {
		a.proxy.Stop()
	}
	if a.app != nil {
		a.client.DeleteApp(a.app.ID)
	}
}

/*
	Make an 'ish' application on the given host, returning it when
	it has registered readiness with discoverd.

	User will want to defer a.Cleanup() to clean up.
*/
func (s *Helper) makeIshApp(t *c.C, a *IshApp) (*IshApp, error) {
	// pick a unique string to use as service name so this works with concurrent tests.
	serviceName := "ish-service-" + random.String(6)

	if a == nil {
		a = &IshApp{}
	}
	if a.cluster != nil {
		a.discoverd = a.cluster.discoverd
		a.client = a.cluster.controller
		if a.host == nil {
			a.host = a.cluster.Host.Host
		}
	} else {
		a.discoverd = s.discoverdClient(t)
		a.client = s.controllerClient(t)
		if a.host == nil {
			a.host = s.anyHostClient(t)
		}
	}

	app := &ct.App{Name: serviceName}
	if err := a.client.CreateApp(app); err != nil {
		a.Cleanup()
		return nil, err
	}
	a.app = app

	// run a job that accepts tcp connections and performs tasks we ask of it in its container
	a.cmd = exec.JobUsingHost(a.host, s.createArtifactWithClient(t, "test-apps", a.client), &host.Job{
		Metadata: map[string]string{"flynn-controller.app": app.ID},
		Config: host.ContainerConfig{
			Args:  []string{"/bin/ish"},
			Ports: []host.Port{{Proto: "tcp"}},
			Env: map[string]string{
				"NAME": serviceName,
			},
		}.Merge(a.extraConfig),
	})
	if err := a.cmd.Start(); err != nil {
		a.Cleanup()
		return nil, err
	}

	// wait for the job to start
	services, err := a.discoverd.Instances(serviceName, time.Second*100)
	if err != nil {
		a.Cleanup()
		return nil, err
	} else if len(services) != 1 {
		a.Cleanup()
		return nil, fmt.Errorf("test setup: expected exactly one service instance, got %d", len(services))
	}

	a.addr = services[0].Addr
	if a.cluster != nil {
		proxy, err := s.clusterProxy(a.cluster, a.addr)
		if err != nil {
			a.Cleanup()
			return nil, err
		}
		a.proxy = proxy
		a.addr = proxy.addr
	}

	return a, nil
}

func (a *IshApp) run(cmd string) (string, error) {
	resp, err := http.Post(
		fmt.Sprintf("http://%s/ish", a.addr),
		"text/plain",
		strings.NewReader(cmd),
	)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func (s *HostSuite) TestNetworkedPersistentJob(t *c.C) {
	// this isn't much more impressive than what's already running by the time we've got a cluster engaged
	// but the idea is to use this basic design to enable testing a series manipulations on a single container.

	// run a job that accepts tcp connections and performs tasks we ask of it in its container
	ish, err := s.makeIshApp(t, nil)
	t.Assert(err, c.IsNil)
	defer ish.Cleanup()

	// test that we can interact with that job
	resp, err := ish.run("echo echocococo")
	t.Assert(err, c.IsNil)
	t.Assert(resp, c.Equals, "echocococo\n")
}

func (s *HostSuite) TestVolumeCreation(t *c.C) {
	h := s.anyHostClient(t)

	vol := &volume.Info{}
	t.Assert(h.CreateVolume("default", vol), c.IsNil)
	t.Assert(vol.ID, c.Not(c.Equals), "")
	t.Assert(h.DestroyVolume(vol.ID), c.IsNil)
}

func (s *HostSuite) TestVolumeCreationFailsForNonexistentProvider(t *c.C) {
	h := s.anyHostClient(t)

	t.Assert(h.CreateVolume("non-existent", &volume.Info{}), c.NotNil)
}

func (s *HostSuite) TestVolumePersistence(t *c.C) {
	// most of the volume tests (snapshotting, quotas, etc) are unit tests under their own package.
	// these tests exist to cover the last mile where volumes are bind-mounted into containers.

	h := s.anyHostClient(t)

	// create a volume!
	vol := &volume.Info{}
	t.Assert(h.CreateVolume("default", vol), c.IsNil)
	defer func() {
		t.Assert(h.DestroyVolume(vol.ID), c.IsNil)
	}()

	// create first job
	ish, err := s.makeIshApp(t, &IshApp{host: h, extraConfig: host.ContainerConfig{
		Volumes: []host.VolumeBinding{{
			Target:    "/vol",
			VolumeID:  vol.ID,
			Writeable: true,
		}},
	}})
	t.Assert(err, c.IsNil)
	defer ish.Cleanup()
	// add data to the volume
	resp, err := ish.run("echo 'testcontent' > /vol/alpha ; echo $?")
	t.Assert(err, c.IsNil)
	t.Assert(resp, c.Equals, "0\n")

	// start another one that mounts the same volume
	ish, err = s.makeIshApp(t, &IshApp{host: h, extraConfig: host.ContainerConfig{
		Volumes: []host.VolumeBinding{{
			Target:    "/vol",
			VolumeID:  vol.ID,
			Writeable: false,
		}},
	}})
	t.Assert(err, c.IsNil)
	defer ish.Cleanup()
	// read data back from the volume
	resp, err = ish.run("cat /vol/alpha")
	t.Assert(err, c.IsNil)
	t.Assert(resp, c.Equals, "testcontent\n")
}

func (s *HostSuite) TestSignalJob(t *c.C) {
	cluster := s.clusterClient(t)

	// pick a host to run the job on
	hosts, err := cluster.Hosts()
	t.Assert(err, c.IsNil)
	client := schedutil.PickHost(hosts)

	// start a signal-service job
	cmd := exec.JobUsingCluster(cluster, s.createArtifact(t, "test-apps"), &host.Job{
		Config: host.ContainerConfig{
			Args:       []string{"/bin/signal"},
			DisableLog: true,
		},
	})
	cmd.HostID = client.ID()
	var out bytes.Buffer
	cmd.Stdout = &out
	t.Assert(cmd.Start(), c.IsNil)
	_, err = s.discoverdClient(t).Instances("signal-service", 10*time.Second)
	t.Assert(err, c.IsNil)

	// send the job a signal
	t.Assert(client.SignalJob(cmd.Job.ID, int(syscall.SIGTERM)), c.IsNil)

	// wait for the job to exit
	done := make(chan error)
	go func() {
		done <- cmd.Wait()
	}()
	select {
	case err := <-done:
		t.Assert(err, c.IsNil)
	case <-time.After(12 * time.Second):
		t.Fatal("timed out waiting for job to stop")
	}

	// check the output
	t.Assert(out.String(), c.Equals, "got signal: terminated")
}

func (s *HostSuite) TestResourceLimits(t *c.C) {
	cmd := exec.JobUsingCluster(
		s.clusterClient(t),
		s.createArtifact(t, "test-apps"),
		&host.Job{
			Config:    host.ContainerConfig{Args: []string{"sh", "-c", resourceCmd}},
			Resources: testResources(),
		},
	)
	var out bytes.Buffer
	cmd.Stdout = &out

	runErr := make(chan error)
	go func() {
		runErr <- cmd.Run()
	}()
	select {
	case err := <-runErr:
		t.Assert(err, c.IsNil)
	case <-time.After(30 * time.Second):
		t.Fatal("timed out waiting for resource limits job")
	}

	assertResourceLimits(t, out.String())
}

func (s *HostSuite) TestDevStdout(t *c.C) {
	cmd := exec.CommandUsingCluster(
		s.clusterClient(t),
		s.createArtifact(t, "test-apps"),
		"sh",
	)
	cmd.Stdin = strings.NewReader(`
echo foo > /dev/stdout
echo bar > /dev/stderr
echo "SUBSHELL: $(echo baz > /dev/stdout)"
echo "SUBSHELL: $(echo qux 2>&1 > /dev/stderr)" >&2`)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := make(chan error)
	go func() {
		runErr <- cmd.Run()
	}()
	select {
	case err := <-runErr:
		t.Assert(err, c.IsNil)
	case <-time.After(30 * time.Second):
		t.Fatal("timed out waiting for /dev/stdout job")
	}

	t.Assert(stdout.String(), c.Equals, "foo\nSUBSHELL: baz\n")
	t.Assert(stderr.String(), c.Equals, "bar\nSUBSHELL: qux\n")
}

func (s *HostSuite) TestDevSHM(t *c.C) {
	cmd := exec.CommandUsingCluster(
		s.clusterClient(t),
		s.createArtifact(t, "test-apps"),
		"sh", "-c", "df -h /dev/shm && echo foo > /dev/shm/asdf",
	)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	runErr := make(chan error)
	go func() {
		runErr <- cmd.Run()
	}()
	select {
	case err := <-runErr:
		t.Assert(err, c.IsNil)
	case <-time.After(30 * time.Second):
		t.Fatal("timed out waiting for /dev/shm job")
	}

	t.Assert(out.String(), c.Equals, "Filesystem                Size      Used Available Use% Mounted on\nshm                      64.0M         0     64.0M   0% /dev/shm\n")
}

func (s *HostSuite) TestNotifyOOM(t *c.C) {
	appID := random.UUID()

	// subscribe to init log messages from the logaggregator
	client, err := logaggc.New("")
	t.Assert(err, c.IsNil)
	opts := logagg.LogOpts{
		Follow:      true,
		StreamTypes: []logagg.StreamType{logagg.StreamTypeInit},
	}
	rc, err := client.GetLog(appID, &opts)
	t.Assert(err, c.IsNil)
	defer rc.Close()
	msgs := make(chan *logaggc.Message)
	stream := stream.New()
	defer stream.Close()
	go func() {
		defer close(msgs)
		dec := json.NewDecoder(rc)
		for {
			var msg logaggc.Message
			if err := dec.Decode(&msg); err != nil {
				stream.Error = err
				return
			}
			select {
			case msgs <- &msg:
			case <-stream.StopCh:
				return
			}
		}
	}()

	// run the OOM job
	cmd := exec.CommandUsingCluster(
		s.clusterClient(t),
		s.createArtifact(t, "test-apps"),
		"/bin/oom",
	)
	cmd.Meta = map[string]string{"flynn-controller.app": appID}
	runErr := make(chan error)
	go func() {
		runErr <- cmd.Run()
	}()

	// wait for the OOM notification
	for {
		select {
		case err := <-runErr:
			t.Assert(err, c.IsNil)
		case msg, ok := <-msgs:
			if !ok {
				t.Fatalf("message stream closed unexpectedly: %s", stream.Err())
			}
			t.Log(msg.Msg)
			if strings.Contains(msg.Msg, "FATAL: a container process was killed due to lack of available memory") {
				return
			}
		case <-time.After(30 * time.Second):
			t.Fatal("timed out waiting for OOM notification")
		}
	}
}

func (s *HostSuite) TestVolumeDeleteOnStop(t *c.C) {
	hosts, err := s.clusterClient(t).Hosts()
	t.Assert(err, c.IsNil)
	t.Assert(hosts, c.Not(c.HasLen), 0)
	h := hosts[0]

	// stream job events so we can wait for cleanup events
	events := make(chan *host.Event)
	stream, err := h.StreamEvents("all", events)
	t.Assert(err, c.IsNil)
	defer stream.Close()
	waitCleanup := func(jobID string) {
		timeout := time.After(30 * time.Second)
		for {
			select {
			case event := <-events:
				if event.JobID == jobID && event.Event == host.JobEventCleanup {
					return
				}
			case <-timeout:
				t.Fatal("timed out waiting for cleanup event")
			}
		}
	}

	for _, deleteOnStop := range []bool{true, false} {
		job := &host.Job{
			Config: host.ContainerConfig{
				Args:       []string{"sh", "-c", "ls -d /foo"},
				DisableLog: true,
			},
		}

		// provision a volume
		req := &ct.VolumeReq{Path: "/foo", DeleteOnStop: deleteOnStop}
		vol, err := utils.ProvisionVolume(req, h, job)
		t.Assert(err, c.IsNil)
		defer h.DestroyVolume(vol.ID)

		// run the job
		cmd := exec.JobUsingCluster(s.clusterClient(t), s.createArtifact(t, "test-apps"), job)
		cmd.HostID = h.ID()
		out, err := cmd.CombinedOutput()
		t.Assert(err, c.IsNil)
		t.Assert(string(out), c.Equals, "/foo\n")

		// wait for a cleanup event
		waitCleanup(job.ID)

		// check if the volume was deleted or not
		vol, err = h.GetVolume(vol.ID)
		if deleteOnStop {
			t.Assert(hh.IsObjectNotFoundError(err), c.Equals, true)
		} else {
			t.Assert(err, c.IsNil)
		}
	}
}

func (s *HostSuite) TestUpdate(t *c.C) {
	dir := t.MkDir()
	flynnHost := filepath.Join(dir, "flynn-host")
	run(t, osexec.Command("cp", args.FlynnHost, flynnHost))

	// start flynn-host
	id := random.String(8)
	var out bytes.Buffer
	cmd := osexec.Command(
		flynnHost,
		"daemon",
		"--http-port", "11113",
		"--state", filepath.Join(dir, "host-state.bolt"),
		"--sink-state", filepath.Join(dir, "sink-state.bolt"),
		"--id", id,
		"--backend", "mock",
		"--vol-provider", "mock",
		"--volpath", filepath.Join(dir, "volumes"),
		"--log-dir", filepath.Join(dir, "logs"),
	)
	cmd.Stdout = &out
	cmd.Stderr = &out
	defer func() {
		debug(t, "*** flynn-host output ***")
		debug(t, out.String())
		debug(t, "*************************")
	}()
	t.Assert(cmd.Start(), c.IsNil)
	defer cmd.Process.Kill()

	httpClient := &http.Client{Transport: &http.Transport{Dial: dialer.Retry.Dial}}
	client := cluster.NewHost(id, "http://127.0.0.1:11113", httpClient, nil)

	// exec a program which exits straight away
	_, err := client.Update("/bin/true")
	t.Assert(err, c.NotNil)
	status, err := client.GetStatus()
	t.Assert(err, c.IsNil)
	t.Assert(status.ID, c.Equals, id)
	t.Assert(status.PID, c.Equals, cmd.Process.Pid)

	// exec a program which reads the control socket but then exits
	_, err = client.Update("/bin/bash", "-c", "<&4; exit")
	t.Assert(err, c.NotNil)
	status, err = client.GetStatus()
	t.Assert(err, c.IsNil)
	t.Assert(status.ID, c.Equals, id)
	t.Assert(status.PID, c.Equals, cmd.Process.Pid)

	// exec flynn-host and check we get the status from the new daemon
	pid, err := client.Update(
		flynnHost,
		"daemon",
		"--http-port", "11113",
		"--state", filepath.Join(dir, "host-state.bolt"),
		"--sink-state", filepath.Join(dir, "sink-state.bolt"),
		"--id", id,
		"--backend", "mock",
		"--vol-provider", "mock",
		"--volpath", filepath.Join(dir, "volumes"),
		"--log-dir", filepath.Join(dir, "logs"),
	)
	t.Assert(err, c.IsNil)
	defer syscall.Kill(pid, syscall.SIGKILL)

	done := make(chan struct{})
	go func() {
		cmd.Process.Signal(syscall.SIGTERM)
		syscall.Wait4(cmd.Process.Pid, nil, 0, nil)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("timed out waiting for flynn-host daemon to exit")
	}

	// client.GetStatus intermittently returns io.EOF right after the update. We
	// don't currently understand why (likely due to the way the listener is
	// passed around), so for now just retry the request.
	//
	// TODO(lmars): figure out why and remove this loop.
	delay := 100 * time.Millisecond
	for start := time.Now(); time.Since(start) < 10*time.Second; time.Sleep(delay) {
		status, err = client.GetStatus()
		if e, ok := err.(*url.Error); ok && strings.Contains(e.Err.Error(), "EOF") {
			debugf(t, "got io.EOF from flynn-host, trying again in %s", delay)
			continue
		}
		break
	}
	t.Assert(err, c.IsNil)
	t.Assert(status.ID, c.Equals, id)
	t.Assert(status.PID, c.Equals, pid)
}

func (s *HostSuite) TestUpdateTags(t *c.C) {
	events := make(chan *discoverd.Event)
	stream, err := s.discoverdClient(t).Service("flynn-host").Watch(events)
	t.Assert(err, c.IsNil)
	defer stream.Close()

	nextEvent := func() *discoverd.Event {
		select {
		case e, ok := <-events:
			if !ok {
				t.Fatal("unexpected close of discoverd stream")
			}
			return e
		case <-time.After(10 * time.Second):
			t.Fatal("timed out waiting for discoverd event")
		}
		return nil
	}

	var client *cluster.Host
	for {
		e := nextEvent()
		if e.Kind == discoverd.EventKindUp && client == nil {
			client = cluster.NewHost(e.Instance.Meta["id"], e.Instance.Addr, nil, nil)
		}
		if e.Kind == discoverd.EventKindCurrent {
			break
		}
	}
	if client == nil {
		t.Fatal("did not initialize flynn-host client")
	}

	t.Assert(client.UpdateTags(map[string]string{"foo": "bar"}), c.IsNil)

	var meta map[string]string
	for {
		e := nextEvent()
		if e.Kind == discoverd.EventKindUpdate && e.Instance.Meta["id"] == client.ID() {
			meta = e.Instance.Meta
			break
		}
	}
	t.Assert(meta["tag:foo"], c.Equals, "bar")

	// setting to empty string should delete the tag
	t.Assert(client.UpdateTags(map[string]string{"foo": ""}), c.IsNil)

	for {
		e := nextEvent()
		if e.Kind == discoverd.EventKindUpdate && e.Instance.Meta["id"] == client.ID() {
			meta = e.Instance.Meta
			break
		}
	}
	if _, ok := meta["tag:foo"]; ok {
		t.Fatal("expected tag to be deleted but is still present")
	}
}

func (s *HostSuite) TestLogSinks(t *c.C) {
	clusterSize := 3
	x := s.bootCluster(t, clusterSize)
	defer x.Destroy()

	// deploy custom logaggregator app
	client := x.controller
	logApp := &ct.App{Name: "test-logaggregator"}
	t.Assert(client.CreateApp(logApp), c.IsNil)
	release, err := client.GetAppRelease("logaggregator")
	t.Assert(err, c.IsNil)
	proc := release.Processes["app"]
	proc.Env = map[string]string{"SERVICE_NAME": logApp.Name}
	proc.Ports[1].Port = 55514
	release.Processes["app"] = proc
	release.ID = ""
	t.Assert(client.CreateRelease(logApp.ID, release), c.IsNil)
	t.Assert(client.SetAppRelease(logApp.ID, release.ID), c.IsNil)
	t.Assert(client.PutFormation(&ct.Formation{
		AppID:     logApp.ID,
		ReleaseID: release.ID,
		Processes: map[string]int{"app": 1},
	}), c.IsNil)
	defer client.DeleteApp(logApp.ID)

	// get hosts
	hosts, err := x.cluster.Hosts()
	t.Assert(err, c.IsNil)
	if len(hosts) != 3 {
		t.Fatal(fmt.Sprintf("unexpected number of hosts, got: %d, expected %d", len(hosts), clusterSize))
	}

	// wait for aggregator to come up and get address
	instances, err := x.discoverd.Instances(logApp.Name, 10*time.Second)
	t.Assert(len(instances), c.Equals, 1)
	logaggAddr := instances[0].Addr

	// logagg proxy
	logHost, _, _ := net.SplitHostPort(logaggAddr)
	logaggProxy, err := s.clusterProxy(x, fmt.Sprintf("%s:%d", logHost, proc.Ports[0].Port))
	t.Assert(err, c.IsNil)

	// add sink to controller
	config, err := json.Marshal(&ct.SyslogSinkConfig{
		URL:            fmt.Sprintf("syslog://%s", instances[0].Addr),
		UseIDs:         true,
		StructuredData: true,
	})
	t.Assert(err, c.IsNil)
	sink := &ct.Sink{
		Kind:   ct.SinkKindSyslog,
		Config: config,
	}
	t.Assert(client.CreateSink(sink), c.IsNil)

	var sinkAttempts = attempt.Strategy{
		Min:   5,
		Total: 10 * time.Second,
		Delay: 200 * time.Millisecond,
	}

	// wait for sink to appear on host
	err = sinkAttempts.Run(func() error {
		sinks, err := hosts[0].GetSinks()
		if err != nil {
			return err
		}
		for _, s := range sinks {
			if s.ID == sink.ID {
				return nil
			}
		}
		return fmt.Errorf("timed out waiting for sink to appear on host")
	})
	t.Assert(err, c.IsNil)

	// create a test app
	app, release := s.createAppWithClient(t, client)
	defer client.DeleteApp(app.ID)

	// subscribe to log messages for the test app from the test logaggregator
	logc, err := logaggc.New("http://" + logaggProxy.addr)
	t.Assert(err, c.IsNil)
	log, err := logc.GetLog(app.ID, &logagg.LogOpts{Follow: true})
	t.Assert(err, c.IsNil)
	defer log.Close()
	msgs := make(chan *logaggc.Message)
	stream := stream.New()
	defer stream.Close()
	go func() {
		defer close(msgs)
		dec := json.NewDecoder(log)
		for {
			var msg logaggc.Message
			if err := dec.Decode(&msg); err != nil {
				stream.Error = err
				return
			}
			select {
			case msgs <- &msg:
			case <-stream.StopCh:
				return
			}
		}
	}()

	// deploy an omni job
	t.Assert(client.PutFormation(&ct.Formation{
		AppID:     app.ID,
		ReleaseID: release.ID,
		Processes: map[string]int{"omni": 1},
	}), c.IsNil)

	// wait for syslog message from each host
	received := make(map[string]struct{})
loop:
	for {
		select {
		case msg := <-msgs:
			received[msg.HostID] = struct{}{}
			if len(received) == len(hosts) {
				break loop
			}
		case <-time.After(30 * time.Second):
			t.Fatal("timed out waiting for log messages")
		}
	}

	// delete the sink
	_, err = client.DeleteSink(sink.ID)
	t.Assert(err, c.IsNil)

	// wait for sink to be removed from host
	err = sinkAttempts.Run(func() error {
		sinks, err := hosts[0].GetSinks()
		if err != nil {
			return err
		}
		for _, s := range sinks {
			if s.ID == sink.ID {
				return fmt.Errorf("timed out waiting for sink to be removed from host")
			}
		}
		return nil
	})
	t.Assert(err, c.IsNil)
}
