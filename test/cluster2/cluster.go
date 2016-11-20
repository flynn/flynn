package cluster2

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"os"
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
	"github.com/flynn/flynn/pkg/tlscert"
	"github.com/opencontainers/runc/libcontainer/configs"
	"gopkg.in/inconshreveable/log15.v2"
)

type BootConfig struct {
	Size         int
	ImagesPath   string
	ManifestPath string
	Logger       log15.Logger
	Client       controller.Client
	HostTimeout  *time.Duration
	Key          string
	Domain       string
}

type Cluster struct {
	App     *ct.App
	Release *ct.Release
	IP      string
	Domain  string
	Key     string
	Pin     string

	config *BootConfig
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

	zfsDev, err := zfsDev()
	if err != nil {
		log.Error("error loading zfs device details", "err", err)
		return nil, err
	}

	release := &ct.Release{
		ArtifactIDs: []string{hostImage.ID},
		Processes: map[string]ct.ProcessType{
			"host": {
				Mounts: []host.Mount{
					{
						Location:  "/var/lib/flynn",
						Target:    "/var/lib/flynn",
						Writeable: true,
					},
					{
						Location: "/dev/zvol",
						Target:   "/dev/zvol",
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
				AllowedDevices: append(host.DefaultAllowedDevices, []*configs.Device{
					{
						Path:        "/dev/zfs",
						Type:        'c',
						Major:       zfsDev.Major,
						Minor:       zfsDev.Minor,
						Permissions: "rwm",
					},
					{
						Type:        'b',
						Major:       230,
						Minor:       configs.Wildcard,
						Permissions: "rwm",
					},
				}...),
				WriteableCgroups: true,
			},
		},
	}
	if err := c.Client.CreateRelease(release); err != nil {
		log.Error("error creating release", "err", err)
		return nil, err
	}
	if err := c.Client.SetAppRelease(app.ID, release.ID); err != nil {
		log.Error("error setting app release", "err", err)
		return nil, err
	}

	formation := &ct.Formation{
		AppID:     app.ID,
		ReleaseID: release.ID,
		Processes: map[string]int{"host": c.Size},
	}
	if err := c.Client.PutFormation(formation); err != nil {
		log.Error("error scaling hosts", "err", err)
		return nil, err
	}

	log.Info("waiting for hosts", "count", c.Size)
	events := make(chan *discoverd.Event)
	stream, err := discoverd.NewService(app.Name).Watch(events)
	if err != nil {
		log.Error("error streaming service events", "err", err)
		return nil, err
	}
	defer stream.Close()

	if c.HostTimeout == nil {
		t := 60 * time.Second
		c.HostTimeout = &t
	}
	timeout := time.After(*c.HostTimeout)

	hosts := make([]*cluster.Host, 0, c.Size)
loop:
	for {
		select {
		case event, ok := <-events:
			if !ok {
				return nil, stream.Err()
			}
			switch event.Kind {
			case discoverd.EventKindUp:
				id, _ := cluster.ExtractUUID(event.Instance.Meta["FLYNN_JOB_ID"])
				id = strings.Replace(id, "-", "", -1)
				addr := event.Instance.Addr
				log.Info("host is up", "addr", addr, "id", id)
				hosts = append(hosts, cluster.NewHost(id, addr, hh.RetryClient, nil))
				if len(hosts) == c.Size {
					break loop
				}
			case discoverd.EventKindDown:
				log.Info("host is down", "addr", event.Instance.Addr, "id", event.Instance.Meta["FLYNN_JOB_ID"])
				return nil, fmt.Errorf("a host failed to start: %v", event.Instance)
			}
		case <-timeout:
			log.Error("timed out waiting for hosts to start")
			return nil, errors.New("timed out waiting for hosts to start")
		}
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

	peerIPs := make([]string, len(hosts))
	for i, host := range hosts {
		ip, _, _ := net.SplitHostPort(host.Addr())
		peerIPs[i] = ip
	}
	log.Info("bootstrapping cluster", "size", c.Size, "domain", domain, "peers", peerIPs)
	cmd := exec.CommandUsingHost(
		hosts[0],
		hostImage,
		"flynn-host",
		"bootstrap",
		"--min-hosts="+strconv.Itoa(c.Size),
		"--peer-ips="+strings.Join(peerIPs, ","),
		"-",
	)
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

	log.Info("successfully bootstrapped cluster", "app", app.Name, "size", c.Size, "peers", peerIPs)
	return &Cluster{
		App:     app,
		Release: release,
		IP:      peerIPs[0],
		Domain:  domain,
		Key:     key,
		Pin:     cert.Pin,
		config:  c,
	}, nil
}

func (c *Cluster) Destroy() error {
	_, err := c.config.Client.DeleteApp(c.App.ID)
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

type ZFSDev struct {
	Major int64
	Minor int64
}

func zfsDev() (*ZFSDev, error) {
	data, err := ioutil.ReadFile("/sys/class/misc/zfs/dev")
	if err != nil {
		return nil, err
	}
	s := strings.SplitN(strings.TrimSpace(string(data)), ":", 2)
	if len(s) != 2 {
		return nil, fmt.Errorf("unexpected data in /sys/class/misc/zfs/dev: %q", data)
	}
	dev := &ZFSDev{}
	dev.Major, err = strconv.ParseInt(s[0], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("error parsing zfs major from %q: %s", data, err)
	}
	dev.Minor, err = strconv.ParseInt(s[1], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("error parsing zfs minor from %q: %s", data, err)
	}
	return dev, nil
}
