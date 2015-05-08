package main

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	c "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
	"github.com/flynn/flynn/cli/config"
	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/pkg/cluster"
	tc "github.com/flynn/flynn/test/cluster"
)

type Helper struct {
	configMtx sync.Mutex
	config    *config.Cluster

	clusterMtx sync.Mutex
	cluster    *cluster.Client

	controllerMtx sync.Mutex
	controller    *controller.Client

	discMtx sync.Mutex
	disc    *discoverd.Client

	hostsMtx sync.Mutex
	hosts    map[string]cluster.Host

	sshMtx sync.Mutex
	ssh    *sshData
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
		client, err := cluster.NewClientWithServices(h.discoverdClient(t).Service)
		t.Assert(err, c.IsNil)
		h.cluster = client
	}
	return h.cluster
}

func (h *Helper) controllerClient(t *c.C) *controller.Client {
	h.controllerMtx.Lock()
	defer h.controllerMtx.Unlock()
	if h.controller == nil {
		conf := h.clusterConf(t)
		pin, err := base64.StdEncoding.DecodeString(conf.TLSPin)
		t.Assert(err, c.IsNil)
		client, err := controller.NewClientWithConfig(conf.URL, conf.Key, controller.Config{Pin: pin})
		t.Assert(err, c.IsNil)
		h.controller = client
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

func (h *Helper) hostClient(t *c.C, hostID string) cluster.Host {
	h.hostsMtx.Lock()
	defer h.hostsMtx.Unlock()
	if h.hosts == nil {
		h.hosts = make(map[string]cluster.Host)
	}
	if client, ok := h.hosts[hostID]; ok {
		return client
	}
	client, err := h.clusterClient(t).DialHost(hostID)
	t.Assert(err, c.IsNil)
	h.hosts[hostID] = client
	return client
}

func (h *Helper) anyHostClient(t *c.C) cluster.Host {
	cluster := h.clusterClient(t)
	hosts, err := cluster.ListHosts()
	t.Assert(err, c.IsNil)
	return h.hostClient(t, hosts[0].ID)
}

func (h *Helper) sshKeys(t *c.C) *sshData {
	h.sshMtx.Lock()
	defer h.sshMtx.Unlock()
	if h.ssh == nil {
		var err error
		h.ssh, err = genSSHKey()
		t.Assert(err, c.IsNil)
	}
	return h.ssh
}

func (h *Helper) createApp(t *c.C) (*ct.App, *ct.Release) {
	client := h.controllerClient(t)

	app := &ct.App{}
	t.Assert(client.CreateApp(app), c.IsNil)
	debugf(t, "created app %s (%s)", app.Name, app.ID)

	artifact := &ct.Artifact{Type: "docker", URI: imageURIs["test-apps"]}
	t.Assert(client.CreateArtifact(artifact), c.IsNil)

	release := &ct.Release{
		ArtifactID: artifact.ID,
		Processes: map[string]ct.ProcessType{
			"echoer": {
				Cmd:     []string{"/bin/echoer"},
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
				Cmd:   []string{"/bin/pingserv"},
				Ports: []ct.Port{{Proto: "tcp"}},
			},
			"printer": {
				Cmd: []string{"sh", "-c", "while true; do echo I like to print; sleep 1; done"},
			},
			"crasher": {
				Cmd: []string{"sh", "-c", "trap 'exit 1' SIGTERM; while true; do echo I like to crash; sleep 1; done"},
			},
			"omni": {
				Cmd:  []string{"sh", "-c", "while true; do echo I am everywhere; sleep 1; done"},
				Omni: true,
			},
			"sequential-printer": {
				Cmd: []string{"sh", "-c", "while true ; do i=$((i + 1)) ; echo $i ; sleep 0.5 ; done"},
			},
		},
	}
	t.Assert(client.CreateRelease(release), c.IsNil)
	t.Assert(client.SetAppRelease(app.ID, release.ID), c.IsNil)
	return app, release
}

func (h *Helper) stopJob(t *c.C, id string) {
	debugf(t, "stopping job %s", id)
	hostID, jobID, _ := cluster.ParseJobID(id)
	hc := h.hostClient(t, hostID)
	t.Assert(hc.StopJob(jobID), c.IsNil)
}

func (h *Helper) addHost(t *c.C) *tc.Instance {
	return h.addHosts(t, 1, false)[0]
}

func (h *Helper) addVanillaHost(t *c.C) *tc.Instance {
	return h.addHosts(t, 1, true)[0]
}

func (h *Helper) addHosts(t *c.C, count int, vanilla bool) []*tc.Instance {
	debugf(t, "adding %d hosts", count)

	ch := make(chan *host.HostEvent)
	stream, err := h.clusterClient(t).StreamHostEvents(ch)
	t.Assert(err, c.IsNil)
	defer stream.Close()

	hosts := make([]*tc.Instance, count)
	for i := 0; i < count; i++ {
		host, err := testCluster.AddHost(ch, vanilla)
		t.Assert(err, c.IsNil)
		debugf(t, "host added: %s", host.ID)
		hosts[i] = host
	}
	return hosts
}

func (h *Helper) removeHost(t *c.C, host *tc.Instance) {
	h.removeHosts(t, []*tc.Instance{host})
}

func (h *Helper) removeHosts(t *c.C, hosts []*tc.Instance) {
	debugf(t, "removing %d hosts", len(hosts))
	for _, host := range hosts {
		t.Assert(testCluster.RemoveHost(host), c.IsNil)
		debugf(t, "host removed: %s", host.ID)
	}
}

type gitRepo struct {
	dir string
	ssh *sshData
	t   *c.C
}

func (h *Helper) newGitRepo(t *c.C, nameOrURL string) *gitRepo {
	dir := filepath.Join(t.MkDir(), "repo")
	r := &gitRepo{dir, h.sshKeys(t), t}

	if strings.HasPrefix(nameOrURL, "https://") {
		t.Assert(run(t, exec.Command("git", "clone", nameOrURL, dir)), Succeeds)
		return r
	}

	t.Assert(run(t, exec.Command("cp", "-r", filepath.Join("apps", nameOrURL), dir)), Succeeds)
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
	cmd.Env = append(os.Environ(), r.ssh.Env...)
	cmd.Dir = r.dir
	return run(r.t, cmd)
}

func (h *Helper) TearDownSuite(t *c.C) {
	h.cleanup()
}

func (h *Helper) cleanup() {
	h.sshMtx.Lock()
	if h.ssh != nil {
		h.ssh.Cleanup()
	}
	h.sshMtx.Unlock()
}
