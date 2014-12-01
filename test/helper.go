package main

import (
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	c "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
	"github.com/flynn/flynn/cli/config"
	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/cluster"
)

type Helper struct {
	config     *config.Cluster
	cluster    *cluster.Client
	controller *controller.Client
	disc       *discoverd.Client
	hosts      map[string]cluster.Host
	ssh        *sshData
}

func (h *Helper) clusterConf(t *c.C) *config.Cluster {
	if h.config == nil {
		conf, err := config.ReadFile(flynnrc)
		t.Assert(err, c.IsNil)
		t.Assert(conf.Clusters, c.HasLen, 1)
		h.config = conf.Clusters[0]
	}
	return h.config
}

func (h *Helper) clusterClient(t *c.C) *cluster.Client {
	if h.cluster == nil {
		var err error
		h.cluster, err = cluster.NewClientWithDial(nil, h.discoverdClient(t).NewServiceSet)
		t.Assert(err, c.IsNil)
	}
	return h.cluster
}

func (h *Helper) controllerClient(t *c.C) *controller.Client {
	if h.controller == nil {
		conf := h.clusterConf(t)
		pin, err := base64.StdEncoding.DecodeString(conf.TLSPin)
		t.Assert(err, c.IsNil)
		h.controller, err = controller.NewClientWithPin(conf.URL, conf.Key, pin)
		t.Assert(err, c.IsNil)
	}
	return h.controller
}

func (h *Helper) discoverdClient(t *c.C) *discoverd.Client {
	if h.disc == nil {
		var err error
		h.disc, err = discoverd.NewClientWithAddr(routerIP + ":1111")
		t.Assert(err, c.IsNil)
	}
	return h.disc
}

func (h *Helper) hostClient(t *c.C, hostID string) cluster.Host {
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

func (h *Helper) sshKeys(t *c.C) *sshData {
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

	artifact := &ct.Artifact{Type: "docker", URI: testImageURIs["test-apps"]}
	t.Assert(client.CreateArtifact(artifact), c.IsNil)

	release := &ct.Release{
		ArtifactID: artifact.ID,
		Processes: map[string]ct.ProcessType{
			"echoer": {
				Cmd:   []string{"/bin/echoer"},
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

func (h *Helper) cleanup() {
	if h.disc != nil {
		h.disc.Close()
	}
	if h.cluster != nil {
		h.cluster.Close()
	}
	if h.controller != nil {
		h.controller.Close()
	}
	for _, host := range h.hosts {
		host.Close()
	}
	if h.ssh != nil {
		h.ssh.Cleanup()
	}
}
