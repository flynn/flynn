package main

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
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
	flynnexec "github.com/flynn/flynn/pkg/exec"
	"github.com/flynn/flynn/pkg/random"
	"github.com/flynn/flynn/pkg/typeconv"
	tc "github.com/flynn/flynn/test/cluster"
	"github.com/flynn/flynn/test/cluster2"
	c "github.com/flynn/go-check"
	"gopkg.in/inconshreveable/log15.v2"
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

type Cluster struct {
	*cluster2.Cluster

	t          *c.C
	discoverd  *discoverd.Client
	cluster    *cluster.Client
	config     *config.Config
	controller controller.Client
	flynnrc    string
	log        log15.Logger
}

func (x *Cluster) Destroy() error {
	if x.t.Failed() {
		if args.Interactive {
			x.runDebugShell()
		} else {
			x.collectDebugInfo()
		}
	}
	Hostnames.Remove(x.t, x.IP)
	return x.Cluster.Destroy()
}

func (x *Cluster) runDebugShell() {
	debugf(x.t, `

*****
starting bash shell with both 'flynn-host' and 'flynn' configured
to debug test failure in cluster %q

run 'exit' when you're done
*****

`, x.App.Name)
	cmd := exec.Command("bash", "--norc")
	cmd.Env = []string{
		fmt.Sprintf("DISCOVERD=http://%s:1111", x.IP),
		fmt.Sprintf("FLYNNRC=%s", x.flynnrc),
		fmt.Sprintf(`PS1=\033[0;36mtest-debug@%s\$\033[0m `, x.App.Name),
	}
	for _, env := range os.Environ() {
		if strings.HasPrefix(env, "DISCOVERD=") || strings.HasPrefix(env, "FLYNNRC=") || strings.HasPrefix(env, "PS1=") {
			continue
		}
		cmd.Env = append(cmd.Env, env)
	}
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Run()
}

func (x *Cluster) flynn(dir string, cmdArgs ...string) *CmdResult {
	cmd := exec.Command(args.CLI, cmdArgs...)
	cmd.Env = flynnEnv(x.flynnrc)
	cmd.Dir = dir
	return run(x.t, cmd)
}

func (x *Cluster) setKey(newKey string) {
	for _, c := range x.config.Clusters {
		c.Key = newKey
	}
	x.t.Assert(x.config.SaveTo(x.flynnrc), c.IsNil)
	x.controller.SetKey(newKey)
}

func (x *Cluster) collectDebugInfo() {
	cmd := flynnexec.CommandUsingHost(
		x.Host.Host,
		x.HostImage,
		"flynn-host",
		"collect-debug-info",
		"--log-dir", filepath.Join("/var/lib/flynn", x.Host.JobID, "logs"),
	)
	cmd.Env = map[string]string{"DISCOVERD": fmt.Sprintf("http://%s:1111", x.IP)}
	cmd.Mounts = []host.Mount{{
		Location: "/var/lib/flynn",
		Target:   "/var/lib/flynn",
	}}
	// stream output to the log
	logR, logW := io.Pipe()
	go func() {
		buf := bufio.NewReader(logR)
		for {
			line, err := buf.ReadString('\n')
			if err != nil {
				return
			}
			x.log.Info(line[0 : len(line)-1])
		}
	}()
	cmd.Stdout = logW
	cmd.Stderr = logW
	// don't allow it to block the tests
	done := make(chan struct{})
	go func() {
		cmd.Run()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
	}
}

func (h *Helper) bootCluster(t *c.C, size int) *Cluster {
	return h.bootClusterWithConfig(t, &cluster2.BootConfig{Size: size})
}

func (h *Helper) bootClusterFromBackup(t *c.C, backup string) *Cluster {
	return h.bootClusterWithConfig(t, &cluster2.BootConfig{Backup: backup, Size: 1})
}

func debugLogWriter(t *c.C) io.Writer {
	r, w := io.Pipe()
	go func() {
		buf := bufio.NewReader(r)
		for {
			line, err := buf.ReadString('\n')
			if err != nil {
				return
			}
			debug(t, line[0:len(line)-1])
		}
	}()
	return w
}

func (h *Helper) bootClusterWithConfig(t *c.C, conf *cluster2.BootConfig) *Cluster {
	conf.ImagesPath = "../images.json"
	conf.ManifestPath = "../bootstrap/bin/manifest.json"
	conf.Client = h.controllerClient(t)

	conf.Logger = log15.New()
	conf.Logger.SetHandler(log15.StreamHandler(debugLogWriter(t), log15.LogfmtFormat()))

	s, err := cluster2.Boot(conf)
	t.Assert(err, c.IsNil)
	x := &Cluster{
		Cluster:   s,
		t:         t,
		discoverd: discoverd.NewClientWithURL(fmt.Sprintf("http://%s:1111", s.IP)),
		flynnrc:   filepath.Join(t.MkDir(), ".flynnrc"),
		log:       conf.Logger,
	}
	x.cluster = cluster.NewClientWithServices(x.discoverd.Service)
	pin, err := base64.StdEncoding.DecodeString(s.Pin)
	t.Assert(err, c.IsNil)
	x.controller, _ = controller.NewClientWithConfig("https://controller."+s.Domain, s.Key, controller.Config{Pin: pin})

	Hostnames.Add(t, s.IP, "controller."+s.Domain, "git."+s.Domain, "docker."+s.Domain, "dashboard."+s.Domain)

	t.Assert(x.flynn("/", "cluster", "add", "--tls-pin", s.Pin, s.Domain, s.Domain, s.Key), Succeeds)

	x.config, err = config.ReadFile(x.flynnrc)
	t.Assert(err, c.IsNil)

	return x
}

type clusterProxy struct {
	addr string
	cmd  *flynnexec.Cmd
}

func (c *clusterProxy) Stop() error {
	return c.cmd.Kill()
}

// clusterProxy starts a TCP proxy inside the cluster listening on the host
// network and proxying to the given internal address (without this there is
// no way for the tests to access internal services like blobstore.discoverd)
func (h *Helper) clusterProxy(x *Cluster, addr string) (*clusterProxy, error) {
	cmd := flynnexec.CommandUsingHost(
		x.Host.Host,
		h.createArtifactWithClient(x.t, "test-apps", x.controller),
		"/bin/proxy",
	)
	service := "cluster-proxy-" + random.String(8)
	cmd.Env = map[string]string{
		"ADDR":    addr,
		"SERVICE": service,
	}
	cmd.HostNetwork = true
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	instances, err := x.discoverd.Instances(service, 30*time.Second)
	if err != nil {
		cmd.Kill()
		return nil, err
	}
	return &clusterProxy{instances[0].Addr, cmd}, nil
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

var Hostnames hostnames

type hostnames struct {
	sync.Mutex
	lastModified time.Time
}

func (h *hostnames) Add(t *c.C, ip string, names ...string) {
	h.Lock()
	defer h.Unlock()
	f, err := os.OpenFile("/etc/hosts", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	t.Assert(err, c.IsNil)
	defer f.Close()
	_, err = fmt.Fprintf(f, "%s %s\n", ip, strings.Join(names, " "))
	t.Assert(err, c.IsNil)

	// wait for the Go resolver's /etc/hosts cache to expire
	if time.Since(h.lastModified) < 5*time.Second {
		time.Sleep(5 * time.Second)
	}
	h.lastModified = time.Now()
}

func (h *hostnames) Remove(t *c.C, ip string) {
	h.Lock()
	defer h.Unlock()
	f, err := os.Open("/etc/hosts")
	t.Assert(err, c.IsNil)
	var data bytes.Buffer
	s := bufio.NewScanner(f)
	for s.Scan() {
		if strings.HasPrefix(s.Text(), ip) {
			continue
		}
		data.Write(append(s.Bytes(), '\n'))
	}
	f.Close()
	t.Assert(s.Err(), c.IsNil)
	t.Assert(ioutil.WriteFile("/etc/hosts", data.Bytes(), 0644), c.IsNil)
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
	return h.createAppWithClient(t, h.controllerClient(t))
}

func (h *Helper) createAppWithClient(t *c.C, client controller.Client) (*ct.App, *ct.Release) {
	app := &ct.App{}
	t.Assert(client.CreateApp(app), c.IsNil)
	debugf(t, "created app %s (%s)", app.Name, app.ID)

	release := &ct.Release{
		ArtifactIDs: []string{h.createArtifactWithClient(t, "test-apps", client).ID},
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
			"minio": {
				Args: []string{"/bin/minio", "server", "/data"},
				Env: map[string]string{
					"MINIO_ACCESS_KEY": minioAccessKey,
					"MINIO_SECRET_KEY": minioSecretKey,
				},
				Ports: []ct.Port{{
					Proto: "tcp",
					Port:  9000,
					Service: &host.Service{
						Name:   "minio",
						Create: true,
						Check:  &host.HealthCheck{Type: "http", Status: 403},
					},
				}},
				Service: "minio",
				Volumes: []ct.VolumeReq{{Path: "/data"}},
			},
		},
	}
	t.Assert(client.CreateRelease(app.ID, release), c.IsNil)
	t.Assert(client.SetAppRelease(app.ID, release.ID), c.IsNil)
	return app, release
}

func (h *Helper) createArtifact(t *c.C, name string) *ct.Artifact {
	return h.createArtifactWithClient(t, name, h.controllerClient(t))
}

func (h *Helper) createArtifactWithClient(t *c.C, name string, client controller.Client) *ct.Artifact {
	path := fmt.Sprintf("../image/%s.json", name)
	manifest, err := ioutil.ReadFile(path)
	t.Assert(err, c.IsNil)
	artifact := &ct.Artifact{
		Type:             ct.ArtifactTypeFlynn,
		URI:              fmt.Sprintf("https://example.com?target=/images/%s.json", name),
		RawManifest:      manifest,
		LayerURLTemplate: "file:///var/lib/flynn/layer-cache/{id}.squashfs",
	}
	t.Assert(client.CreateArtifact(artifact), c.IsNil)
	return artifact
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

func (h *Helper) testBuildCaching(t *c.C, x *Cluster) {
	r := h.newGitRepo(t, "build-cache")
	r.cluster = x
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
	dir     string
	t       *c.C
	cluster *Cluster
	trace   bool
}

func (h *Helper) newGitRepo(t *c.C, nameOrURL string) *gitRepo {
	return h.newGitRepoWithTrace(t, nameOrURL, true)
}

func (h *Helper) newGitRepoWithoutTrace(t *c.C, nameOrURL string) *gitRepo {
	return h.newGitRepoWithTrace(t, nameOrURL, false)
}

func (h *Helper) newGitRepoWithTrace(t *c.C, nameOrURL string, trace bool) *gitRepo {
	dir := filepath.Join(t.MkDir(), "repo")
	r := &gitRepo{dir: dir, t: t, trace: trace}

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
	if r.cluster != nil {
		return r.cluster.flynn(r.dir, args...)
	}
	return flynn(r.t, r.dir, args...)
}

func (r *gitRepo) git(args ...string) *CmdResult {
	cmd := exec.Command("git", args...)
	cmd.Env = os.Environ()
	if r.cluster != nil {
		cmd.Env = flynnEnv(r.cluster.flynnrc)
	}
	if r.trace {
		cmd.Env = append(cmd.Env, "GIT_TRACE=1", "GIT_TRACE_PACKET=1", "GIT_CURL_VERBOSE=1")
	}
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
