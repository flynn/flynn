package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/docker/go-units"
	"github.com/flynn/flynn/cli/config"
	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/host/resource"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/pkg/cluster"
	"github.com/flynn/flynn/pkg/typeconv"
	tc "github.com/flynn/flynn/test/cluster"
	c "github.com/flynn/go-check"
)

type Helper struct {
	configMtx sync.Mutex
	config    *config.Cluster

	clusterMtx sync.Mutex
	cluster    *cluster.Client

	controllerMtx sync.Mutex
	controller    controller.Client

	discMtx sync.Mutex
	disc    *discoverd.Client

	hostsMtx sync.Mutex
	hosts    map[string]*cluster.Host
}

func (h *Helper) clusterConf(t *c.C) *config.Cluster {
	h.configMtx.Lock()
	defer h.configMtx.Unlock()
	if h.config == nil {
		conf, err := config.ReadFile(flynnrc)
		t.Assert(err, c.IsNil)
		t.Assert(conf.Clusters, c.HasLen, 1)
		h.config = conf.Clusters[0]
	}
	return h.config
}

func (h *Helper) clusterClient(t *c.C) *cluster.Client {
	h.clusterMtx.Lock()
	defer h.clusterMtx.Unlock()
	if h.cluster == nil {
		h.cluster = cluster.NewClientWithServices(h.discoverdClient(t).Service)
	}
	return h.cluster
}

func (h *Helper) controllerClient(t *c.C) controller.Client {
	h.controllerMtx.Lock()
	defer h.controllerMtx.Unlock()
	if h.controller == nil {
		conf := h.clusterConf(t)
		var err error
		h.controller, err = conf.Client()
		t.Assert(err, c.IsNil)
	}
	return h.controller
}

func (h *Helper) discoverdClient(t *c.C) *discoverd.Client {
	h.discMtx.Lock()
	defer h.discMtx.Unlock()
	if h.disc == nil {
		h.disc = discoverd.NewClientWithURL(fmt.Sprintf("http://%s:1111", routerIP))
	}
	return h.disc
}

func (h *Helper) hostClient(t *c.C, hostID string) *cluster.Host {
	h.hostsMtx.Lock()
	defer h.hostsMtx.Unlock()
	if h.hosts == nil {
		h.hosts = make(map[string]*cluster.Host)
	}
	if client, ok := h.hosts[hostID]; ok {
		return client
	}
	client, err := h.clusterClient(t).Host(hostID)
	t.Assert(err, c.IsNil)
	h.hosts[hostID] = client
	return client
}

func (h *Helper) anyHostClient(t *c.C) *cluster.Host {
	cluster := h.clusterClient(t)
	hosts, err := cluster.Hosts()
	t.Assert(err, c.IsNil)
	return hosts[0]
}

const (
	resourceMem   int64 = 256 * units.MiB
	resourceMaxFD int64 = 1024
	resourceCmd         = "cat /sys/fs/cgroup/memory/memory.limit_in_bytes; cat /sys/fs/cgroup/cpu/cpu.shares; ulimit -n"
)

func testResources() resource.Resources {
	r := resource.Resources{
		resource.TypeMemory: resource.Spec{Limit: typeconv.Int64Ptr(resourceMem)},
		resource.TypeCPU:    resource.Spec{Limit: typeconv.Int64Ptr(750)},
		resource.TypeMaxFD:  resource.Spec{Limit: typeconv.Int64Ptr(resourceMaxFD)},
	}
	resource.SetDefaults(&r)
	return r
}

func assertResourceLimits(t *c.C, out string) {
	limits := strings.Split(strings.TrimSpace(out), "\n")
	t.Assert(limits, c.HasLen, 3)
	t.Assert(limits[0], c.Equals, strconv.FormatInt(resourceMem, 10))
	t.Assert(limits[1], c.Equals, strconv.FormatInt(768, 10))
	t.Assert(limits[2], c.Equals, strconv.FormatInt(resourceMaxFD, 10))
}

func (h *Helper) createApp(t *c.C) (*ct.App, *ct.Release) {
	client := h.controllerClient(t)

	app := &ct.App{}
	t.Assert(client.CreateApp(app), c.IsNil)
	debugf(t, "created app %s (%s)", app.Name, app.ID)

	artifact := &ct.Artifact{Type: host.ArtifactTypeDocker, URI: imageURIs["test-apps"]}
	t.Assert(client.CreateArtifact(artifact), c.IsNil)

	release := &ct.Release{
		ArtifactIDs: []string{artifact.ID},
		Processes: map[string]ct.ProcessType{
			"echoer": {
				Args:    []string{"/bin/echoer"},
				Service: "echo-service",
				Ports: []ct.Port{{
					Proto: "tcp",
					Service: &host.Service{
						Name:   "echo-service",
						Create: true,
					},
				}},
			},
			"ping": {
				Args:  []string{"/bin/pingserv"},
				Ports: []ct.Port{{Proto: "tcp"}},
			},
			"printer": {
				Args: []string{"sh", "-c", "while true; do echo I like to print; sleep 1; done"},
			},
			"crasher": {
				Args: []string{"sh", "-c", "trap 'exit 1' SIGTERM; while true; do echo I like to crash; sleep 1; done"},
			},
			"omni": {
				Args: []string{"sh", "-c", "while true; do echo I am everywhere; sleep 1; done"},
				Omni: true,
			},
			"resources": {
				Args:      []string{"sh", "-c", resourceCmd},
				Resources: testResources(),
			},
			"ish": {
				Args:  []string{"/bin/ish"},
				Ports: []ct.Port{{Proto: "tcp"}},
				Env: map[string]string{
					"NAME": app.Name,
				},
			},
			"blocker": {
				Args: []string{"/bin/http-blocker"},
				Ports: []ct.Port{{
					Proto: "tcp",
					Service: &host.Service{
						Name:   "test-http-blocker",
						Create: true,
					},
				}},
			},
		},
	}
	t.Assert(client.CreateRelease(release), c.IsNil)
	t.Assert(client.SetAppRelease(app.ID, release.ID), c.IsNil)
	return app, release
}

func (h *Helper) stopJob(t *c.C, id string) {
	debugf(t, "stopping job %s", id)
	hostID, _ := cluster.ExtractHostID(id)
	hc := h.hostClient(t, hostID)
	t.Assert(hc.StopJob(id), c.IsNil)
}

func (h *Helper) addHost(t *c.C, service string) *tc.Instance {
	return h.addHosts(t, 1, false, service)[0]
}

func (h *Helper) addHosts(t *c.C, count int, vanilla bool, service string) []*tc.Instance {
	debugf(t, "adding %d hosts", count)

	// wait for the router-api to start on the host (rather than using
	// StreamHostEvents) as we wait for router-api when removing the
	// host (so that could fail if the router-api never starts).
	events := make(chan *discoverd.Event)
	stream, err := h.discoverdClient(t).Service(service).Watch(events)
	t.Assert(err, c.IsNil)
	defer stream.Close()

	// wait for the current state
loop:
	for {
		select {
		case e, ok := <-events:
			if !ok {
				t.Fatal("event stream closed unexpectedly")
			}
			if e.Kind == discoverd.EventKindCurrent {
				break loop
			}
		case <-time.After(10 * time.Second):
			t.Fatal("timed out waiting for current service state")
		}
	}

	hosts := make([]*tc.Instance, count)
	for i := 0; i < count; i++ {
		host, err := testCluster.AddHost(events, vanilla)
		t.Assert(err, c.IsNil)
		debugf(t, "host added: %s", host.ID)
		hosts[i] = host
	}
	return hosts
}

func (h *Helper) removeHost(t *c.C, host *tc.Instance, service string) {
	h.removeHosts(t, []*tc.Instance{host}, service)
}

func (h *Helper) removeHosts(t *c.C, hosts []*tc.Instance, service string) {
	debugf(t, "removing %d hosts", len(hosts))

	// Clean shutdown requires waiting for that host to unadvertise on discoverd.
	// Specifically: Wait for router-api services to disappear to indicate host
	// removal (rather than using StreamHostEvents), so that other
	// tests won't try and connect to this host via service discovery.
	events := make(chan *discoverd.Event)
	stream, err := h.discoverdClient(t).Service(service).Watch(events)
	t.Assert(err, c.IsNil)
	defer stream.Close()

	for _, host := range hosts {
		t.Assert(testCluster.RemoveHost(events, host), c.IsNil)
		debugf(t, "host removed: %s", host.ID)
	}
}

func (h *Helper) assertURI(t *c.C, uri string, status int) {
	req, err := http.NewRequest("HEAD", uri, nil)
	t.Assert(err, c.IsNil)
	res, err := http.DefaultClient.Do(req)
	t.Assert(err, c.IsNil)
	res.Body.Close()
	t.Assert(res.StatusCode, c.Equals, status)
}

func (h *Helper) buildDockerImage(t *c.C, repo string, lines ...string) {
	cmd := exec.Command("docker", "build", "--tag", repo, "-")
	cmd.Stdin = bytes.NewReader([]byte(fmt.Sprintf("FROM flynn/test-apps\n%s\n", strings.Join(lines, "\n"))))
	t.Assert(run(t, cmd), Succeeds)
}

func (h *Helper) testBuildCaching(t *c.C) {
	r := h.newGitRepo(t, "build-cache")
	t.Assert(r.flynn("create"), Succeeds)
	t.Assert(r.flynn("env", "set", "BUILDPACK_URL=https://github.com/kr/heroku-buildpack-inline"), Succeeds)

	r.git("commit", "-m", "bump", "--allow-empty")
	push := r.git("push", "flynn", "master")
	t.Assert(push, Succeeds)
	t.Assert(push, c.Not(OutputContains), "cached")

	r.git("commit", "-m", "bump", "--allow-empty")
	push = r.git("push", "flynn", "master")
	t.Assert(push, SuccessfulOutputContains, "cached: 0")

	r.git("commit", "-m", "bump", "--allow-empty")
	push = r.git("push", "flynn", "master")
	t.Assert(push, SuccessfulOutputContains, "cached: 1")
}

type gitRepo struct {
	dir string
	t   *c.C
}

func (h *Helper) newGitRepo(t *c.C, nameOrURL string) *gitRepo {
	dir := filepath.Join(t.MkDir(), "repo")
	r := &gitRepo{dir, t}

	if strings.HasPrefix(nameOrURL, "https://") {
		t.Assert(run(t, exec.Command("git", "clone", nameOrURL, dir)), Succeeds)
		return r
	} else if nameOrURL != "" {
		t.Assert(run(t, exec.Command("cp", "-r", filepath.Join("apps", nameOrURL), dir)), Succeeds)
	} else {
		t.Assert(os.Mkdir(dir, 0755), c.IsNil)
		t.Assert(ioutil.WriteFile(filepath.Join(dir, "file.txt"), []byte("app"), 0644), c.IsNil)
	}

	t.Assert(r.git("init"), Succeeds)
	t.Assert(r.git("add", "."), Succeeds)
	t.Assert(r.git("commit", "-am", "init"), Succeeds)
	return r
}

func (r *gitRepo) flynn(args ...string) *CmdResult {
	return flynn(r.t, r.dir, args...)
}

func (r *gitRepo) git(args ...string) *CmdResult {
	cmd := exec.Command("git", args...)
	cmd.Dir = r.dir
	return run(r.t, cmd)
}

func (r *gitRepo) sh(command string) *CmdResult {
	cmd := exec.Command("sh", "-c", command)
	cmd.Dir = r.dir
	return run(r.t, cmd)
}

func (h *Helper) newCliTestApp(t *c.C) *cliTestApp {
	app, release := h.createApp(t)
	watcher, err := h.controllerClient(t).WatchJobEvents(app.Name, release.ID)
	t.Assert(err, c.IsNil)
	return &cliTestApp{
		id:      app.ID,
		name:    app.Name,
		release: release,
		disc:    h.discoverdClient(t),
		t:       t,
		watcher: watcher,
	}
}

type cliTestApp struct {
	id, name string
	release  *ct.Release
	watcher  ct.JobWatcher
	disc     *discoverd.Client
	t        *c.C
}

func (a *cliTestApp) flynn(args ...string) *CmdResult {
	return flynn(a.t, "/", append([]string{"-a", a.name}, args...)...)
}

func (a *cliTestApp) flynnCmd(args ...string) *exec.Cmd {
	return flynnCmd("/", append([]string{"-a", a.name}, args...)...)
}

func (a *cliTestApp) waitFor(events ct.JobEvents) string {
	var id string
	idSetter := func(e *ct.Job) error {
		id = e.ID
		return nil
	}

	a.t.Assert(a.watcher.WaitFor(events, scaleTimeout, idSetter), c.IsNil)
	return id
}

func (a *cliTestApp) waitForService(name string) {
	_, err := a.disc.Instances(name, 30*time.Second)
	a.t.Assert(err, c.IsNil)
}

func (a *cliTestApp) sh(cmd string) *CmdResult {
	return a.flynn("run", "sh", "-c", cmd)
}

func (a *cliTestApp) cleanup() {
	a.watcher.Close()
}
