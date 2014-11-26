package main

import (
	"encoding/base64"

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

func (h *Helper) newSlugrunnerArtifact(t *c.C) *ct.Artifact {
	r, err := h.controllerClient(t).GetAppRelease("gitreceive")
	t.Assert(err, c.IsNil)
	slugrunnerURI := r.Processes["app"].Env["SLUGRUNNER_IMAGE_URI"]
	t.Assert(slugrunnerURI, c.Not(c.Equals), "")
	return &ct.Artifact{Type: "docker", URI: slugrunnerURI}
}

func (h *Helper) createApp(t *c.C) (*ct.App, *ct.Release) {
	client := h.controllerClient(t)

	app := &ct.App{}
	t.Assert(client.CreateApp(app), c.IsNil)
	debugf(t, "created app %s (%s)", app.Name, app.ID)

	artifact := h.newSlugrunnerArtifact(t)
	t.Assert(client.CreateArtifact(artifact), c.IsNil)

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
	t.Assert(client.CreateRelease(release), c.IsNil)
	t.Assert(client.SetAppRelease(app.ID, release.ID), c.IsNil)
	return app, release
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
	for _, h := range h.hosts {
		h.Close()
	}
}
