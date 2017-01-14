package cluster2

import (
	"bufio"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/pkg/cluster"
	"github.com/flynn/flynn/pkg/exec"
	hh "github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/random"
	"github.com/flynn/flynn/pkg/schedutil"
	"github.com/flynn/flynn/pkg/tlscert"
	"github.com/inconshreveable/log15"
)

type BootConfig struct {
	Size         int
	Backup       string
	ImagesPath   string
	ManifestPath string
	Logger       log15.Logger
	Client       controller.Client
	HostTimeout  *time.Duration
	Key          string
	Domain       string
}

type Host struct {
	*cluster.Host
	JobID string
	IP    string
}

type Cluster struct {
	App       *ct.App
	Release   *ct.Release
	HostImage *ct.Artifact
	Hosts     map[string]*Host
	Host      *Host
	IP        string
	Domain    string
	Key       string
	Pin       string

	config      *BootConfig
	size        int
	clusterHost *cluster.Host
	log         log15.Logger
}

func Boot(c *BootConfig) (*Cluster, error) {
	if c.Size <= 0 || c.Size == 2 {
		return nil, errors.New("size must be either 1 or >= 3")
	}

	if c.ImagesPath == "" {
		return nil, errors.New("missing images path")
	}

	if c.ManifestPath == "" {
		return nil, errors.New("missing manifest path")
	}

	// run all jobs on a single host as the overlay network doesn't work
	// when spread across multiple hosts
	hosts, err := cluster.NewClient().Hosts()
	if err != nil {
		return nil, err
	} else if len(hosts) == 0 {
		return nil, errors.New("no hosts")
	}
	clusterHost := schedutil.PickHost(hosts)
	if _, ok := clusterHost.Tags()["host_id"]; !ok {
		return nil, errors.New("missing host_id tag")
	}

	log := c.Logger
	if log == nil {
		log = log15.New()
	}
	log.Info("booting cluster", "size", c.Size)

	manifest, err := os.Open(c.ManifestPath)
	if err != nil {
		log.Error("error loading bootstrap manifest", "err", err)
		return nil, err
	}
	defer manifest.Close()

	if c.Client == nil {
		var err error
		client, err := controllerClient()
		if err != nil {
			log.Error("error creating controller client", "err", err)
			return nil, err
		}
		c.Client = client
	}

	hostImage, err := loadHostImage(c.ImagesPath)
	if err != nil {
		log.Error("error loading host image", "err", err)
		return nil, err
	}
	if err := c.Client.CreateArtifact(hostImage); err != nil {
		log.Error("error creating host image", "err", err)
		return nil, err
	}

	app := &ct.App{Name: "flynn-" + random.String(4)}
	log.Info("creating app", "name", app.Name)
	if err := c.Client.CreateApp(app); err != nil {
		log.Error("error creating app", "err", err)
		return nil, err
	}

	var hostEnv map[string]string
	if c.Size >= 3 {
		hostEnv = map[string]string{"DISCOVERY_SERVICE": app.Name}
	}
	release := &ct.Release{
		ArtifactIDs: []string{hostImage.ID},
		Processes: map[string]ct.ProcessType{
			"host": {
				Env:      hostEnv,
				Profiles: []host.JobProfile{host.JobProfileZFS},
				Mounts: []host.Mount{
					{
						Location:  "/var/lib/flynn",
						Target:    "/var/lib/flynn",
						Writeable: true,
					},
				},
				Ports: []ct.Port{{
					Port:  1113,
					Proto: "tcp",
					Service: &host.Service{
						Name:   app.Name,
						Create: true,
						Check:  &host.HealthCheck{Type: "tcp"},
					},
				}},
				LinuxCapabilities: append(host.DefaultCapabilities, []string{
					"CAP_SYS_ADMIN",
					"CAP_NET_ADMIN",
				}...),
				WriteableCgroups: true,
			},
		},
	}
	if c.Backup != "" {
		proc := release.Processes["host"]
		proc.Mounts = append(proc.Mounts, host.Mount{
			Location: c.Backup,
			Target:   c.Backup,
		})
		release.Processes["host"] = proc
	}
	if err := c.Client.CreateRelease(app.ID, release); err != nil {
		log.Error("error creating release", "err", err)
		return nil, err
	}
	if err := c.Client.SetAppRelease(app.ID, release.ID); err != nil {
		log.Error("error setting app release", "err", err)
		return nil, err
	}

	if err := discoverd.DefaultClient.AddService(app.Name, nil); err != nil {
		log.Error("error creating service", "err", err)
		return nil, err
	}
	serviceData, _ := json.Marshal(&struct{ Size int }{c.Size})
	if err := discoverd.NewService(app.Name).SetMeta(&discoverd.ServiceMeta{Data: serviceData}); err != nil {
		log.Error("error setting service metadata", "err", err)
		return nil, err
	}

	cluster := &Cluster{
		App:         app,
		Release:     release,
		HostImage:   hostImage,
		config:      c,
		clusterHost: clusterHost,
		log:         log,
	}

	if _, err := cluster.AddHosts(c.Size); err != nil {
		return nil, err
	}

	domain := c.Domain
	if domain == "" {
		domain = random.String(32) + ".local"
	}
	cert, err := tlscert.Generate([]string{domain, "*." + domain})
	if err != nil {
		log.Error("error generating TLS certs", "err", err)
		return nil, err
	}

	peerIPs := make([]string, 0, len(cluster.Hosts))
	for _, host := range cluster.Hosts {
		peerIPs = append(peerIPs, host.IP)
	}
	log.Info("bootstrapping cluster", "size", c.Size, "domain", domain, "peers", peerIPs, "backup", filepath.Base(c.Backup))
	bootstrapArgs := []string{
		"--min-hosts", strconv.Itoa(c.Size),
		"--peer-ips", strings.Join(peerIPs, ","),
	}
	if c.Backup != "" {
		bootstrapArgs = append(bootstrapArgs, "--from-backup", c.Backup)
	}
	bootstrapArgs = append(bootstrapArgs, "-")
	cmd := exec.CommandUsingHost(cluster.Host.Host, hostImage, append([]string{"flynn-host", "bootstrap"}, bootstrapArgs...)...)
	if c.Backup != "" {
		cmd.Mounts = []host.Mount{{
			Location: c.Backup,
			Target:   c.Backup,
		}}
	}
	key := c.Key
	if key == "" {
		key = random.String(32)
	}
	discURL := fmt.Sprintf("http://%s:1111", peerIPs[0])
	cmd.Env = map[string]string{
		"CLUSTER_DOMAIN":  domain,
		"CONTROLLER_KEY":  key,
		"DISCOVERD":       discURL,
		"FLANNEL_NETWORK": "100.64.0.0/16",
		"TLS_CA":          cert.CACert,
		"TLS_KEY":         cert.PrivateKey,
		"TLS_CERT":        cert.Cert,
	}
	cmd.HostNetwork = true
	cmd.Stdin = manifest
	// stream output to the log
	logR, logW := io.Pipe()
	go func() {
		buf := bufio.NewReader(logR)
		for {
			line, err := buf.ReadString('\n')
			if err != nil {
				return
			}
			log.Info(line[0 : len(line)-1])
		}
	}()
	cmd.Stdout = logW
	cmd.Stderr = logW
	if err := cmd.Run(); err != nil {
		log.Error("error bootstrapping cluster", "err", err)
		return nil, err
	}

	if c.Backup != "" {
		jobs, err := cluster.Host.ListJobs()
		if err != nil {
			log.Error("error getting job list", "err", err)
			return nil, err
		}
		for _, job := range jobs {
			app := job.Job.Metadata["flynn-controller.app_name"]
			typ := job.Job.Metadata["flynn-controller.type"]
			if app == "controller" && typ == "web" {
				domain = job.Job.Config.Env["DEFAULT_ROUTE_DOMAIN"]
				key = job.Job.Config.Env["AUTH_KEY"]
			}
			if app == "router" && typ == "app" {
				b, _ := pem.Decode([]byte(job.Job.Config.Env["TLSCERT"]))
				sha := sha256.Sum256(b.Bytes)
				cert.Pin = base64.StdEncoding.EncodeToString(sha[:])
			}
		}
		log.Info("retrieved cluster domain, pin and key from backup", "domain", domain, "key", key, "pin", cert.Pin)
	}

	log.Info("successfully bootstrapped cluster", "app", app.Name, "size", c.Size, "peers", peerIPs)
	cluster.IP = peerIPs[0]
	cluster.Domain = domain
	cluster.Key = key
	cluster.Pin = cert.Pin
	return cluster, nil
}

func (c *Cluster) AddHosts(count int) ([]*cluster.Host, error) {
	c.log.Info("adding hosts", "count", count)
	events := make(chan *discoverd.Event)
	stream, err := discoverd.NewService(c.App.Name).Watch(events)
	if err != nil {
		c.log.Error("error streaming service events", "err", err)
		return nil, err
	}
	defer stream.Close()

	timeout := 60 * time.Second
	if c.config.HostTimeout != nil {
		timeout = *c.config.HostTimeout
	}
	timeoutCh := time.After(timeout)

	// wait for the current hosts before scaling up
loop:
	for {
		select {
		case event, ok := <-events:
			if !ok {
				return nil, stream.Err()
			}
			if event.Kind == discoverd.EventKindCurrent {
				break loop
			}
		case <-timeoutCh:
			c.log.Error("timed out waiting for current hosts")
			return nil, errors.New("timed out waiting for current hosts")
		}
	}

	c.size += count
	formation := &ct.Formation{
		AppID:     c.App.ID,
		ReleaseID: c.Release.ID,
		Processes: map[string]int{"host": c.size},
		Tags:      map[string]map[string]string{"host": c.clusterHost.Tags()},
	}
	if err := c.config.Client.PutFormation(formation); err != nil {
		c.log.Error("error scaling hosts", "err", err)
		return nil, err
	}

	newHosts := make([]*cluster.Host, 0, count)
	if c.Hosts == nil {
		c.Hosts = make(map[string]*Host, count)
	}
	for {
		select {
		case event, ok := <-events:
			if !ok {
				return nil, stream.Err()
			}
			switch event.Kind {
			case discoverd.EventKindUp:
				jobID := event.Instance.Meta["FLYNN_JOB_ID"]
				id, _ := cluster.ExtractUUID(jobID)
				id = strings.Replace(id, "-", "", -1)
				addr := event.Instance.Addr
				ip, _, _ := net.SplitHostPort(addr)
				c.log.Info("host is up", "addr", addr, "id", id)
				host := &Host{
					Host:  cluster.NewHost(id, addr, hh.RetryClient, nil),
					JobID: jobID,
					IP:    ip,
				}
				newHosts = append(newHosts, host.Host)
				c.Hosts[id] = host
				if c.Host == nil {
					c.Host = host
				}
				if len(newHosts) == count {
					return newHosts, nil
				}
			case discoverd.EventKindDown:
				c.log.Info("host is down", "addr", event.Instance.Addr, "id", event.Instance.Meta["FLYNN_JOB_ID"])
				return nil, fmt.Errorf("a host failed to start: %v", event.Instance)
			}
		case <-timeoutCh:
			c.log.Error("timed out waiting for hosts to start")
			return nil, errors.New("timed out waiting for hosts to start")
		}
	}
}

func (c *Cluster) Destroy() error {
	log := c.log.New("fn", "Destroy", "app", c.App.Name)

	// scale down and delete the zpools before deleting the app
	watcher, err := c.config.Client.WatchJobEvents(c.App.Name, c.Release.ID)
	if err != nil {
		log.Error("error watching job events", "err", err)
		return err
	}
	defer watcher.Close()

	if err := c.config.Client.PutFormation(&ct.Formation{
		AppID:     c.App.ID,
		ReleaseID: c.Release.ID,
		Processes: map[string]int{"host": 0},
	}); err != nil {
		log.Error("error scaling formation down", "err", err)
		return err
	}

	if err := watcher.WaitFor(ct.JobEvents{"host": ct.JobDownEvents(c.size)}, 30*time.Second, nil); err != nil {
		log.Error("error waiting for hosts to stop", "err", err)
		return err
	}

	proc := c.Release.Processes["host"]
	for _, host := range c.Hosts {
		log.Info("running host cleanup", "job.id", host.JobID)
		cmd := exec.CommandUsingHost(c.clusterHost, c.HostImage, "/usr/local/bin/cleanup-flynn-host.sh", host.JobID)
		logR, logW := io.Pipe()
		go func() {
			buf := bufio.NewReader(logR)
			for {
				line, err := buf.ReadString('\n')
				if err != nil {
					return
				}
				log.Info(line[0 : len(line)-1])
			}
		}()
		cmd.Stdout = logW
		cmd.Stderr = logW
		cmd.Mounts = proc.Mounts
		cmd.Profiles = proc.Profiles
		if err := cmd.Run(); err != nil {
			log.Error("error running host cleanup", "job.id", host.JobID, "err", err)
			continue
		}
	}

	_, err = c.config.Client.DeleteApp(c.App.ID)
	return err
}

func controllerClient() (controller.Client, error) {
	instances, err := discoverd.NewService("controller").Instances()
	if err != nil {
		return nil, err
	}
	inst := instances[0]
	return controller.NewClient("http://"+inst.Addr, inst.Meta["AUTH_KEY"])
}

func loadHostImage(path string) (*ct.Artifact, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var artifacts map[string]*ct.Artifact
	if err := json.NewDecoder(f).Decode(&artifacts); err != nil {
		return nil, err
	}
	artifact, ok := artifacts["host"]
	if !ok {
		return nil, errors.New("missing host image from images.json")
	}
	return artifact, nil
}
