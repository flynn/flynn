package main

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/alexzorin/libvirt-go"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/docker/docker/daemon/networkdriver/ipallocator"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/docker/docker/pkg/term"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/docker/libcontainer/netlink"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/miekg/dns"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/natefinch/lumberjack"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/technoweenie/grohl"
	"github.com/flynn/flynn/host/containerinit"
	lt "github.com/flynn/flynn/host/libvirt"
	"github.com/flynn/flynn/host/logbuf"
	"github.com/flynn/flynn/host/logmux"
	"github.com/flynn/flynn/host/resource"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/host/volume/manager"
	"github.com/flynn/flynn/pinkerton"
	"github.com/flynn/flynn/pinkerton/layer"
	"github.com/flynn/flynn/pkg/attempt"
	"github.com/flynn/flynn/pkg/cluster"
	"github.com/flynn/flynn/pkg/iptables"
	"github.com/flynn/flynn/pkg/random"
)

const (
	libvirtNetName = "flynn"
	bridgeName     = "flynnbr0"
	imageRoot      = "/var/lib/docker"
)

func NewLibvirtLXCBackend(state *State, vman *volumemanager.Manager, volPath, logPath, initPath string, mux *logmux.LogMux) (Backend, error) {
	libvirtc, err := libvirt.NewVirConnection("lxc:///")
	if err != nil {
		return nil, err
	}

	pinkertonCtx, err := pinkerton.BuildContext("aufs", imageRoot)
	if err != nil {
		return nil, err
	}

	return &LibvirtLXCBackend{
		LogPath:    logPath,
		VolPath:    volPath,
		InitPath:   initPath,
		libvirt:    libvirtc,
		state:      state,
		vman:       vman,
		pinkerton:  pinkertonCtx,
		logs:       make(map[string]*logbuf.Log),
		containers: make(map[string]*libvirtContainer),
		defaultEnv: make(map[string]string),
		resolvConf: "/etc/resolv.conf",
		mux:        mux,
		ipalloc:    ipallocator.New(),
	}, nil
}

type LibvirtLXCBackend struct {
	LogPath   string
	InitPath  string
	VolPath   string
	libvirt   libvirt.VirConnection
	state     *State
	vman      *volumemanager.Manager
	pinkerton *pinkerton.Context
	ipalloc   *ipallocator.IPAllocator

	ifaceMTU   int
	bridgeAddr net.IP
	bridgeNet  *net.IPNet
	resolvConf string

	logsMtx sync.Mutex
	logs    map[string]*logbuf.Log
	mux     *logmux.LogMux

	containersMtx sync.RWMutex
	containers    map[string]*libvirtContainer

	envMtx     sync.RWMutex
	defaultEnv map[string]string
}

type libvirtContainer struct {
	RootPath string
	IP       net.IP
	job      *host.Job
	l        *LibvirtLXCBackend
	done     chan struct{}
	*containerinit.Client
}

type dockerImageConfig struct {
	User       string
	Env        []string
	Cmd        []string
	Entrypoint []string
	WorkingDir string
	Volumes    map[string]struct{}
}

func writeContainerConfig(path string, c *containerinit.Config, envs ...map[string]string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	c.Env = make(map[string]string)
	for _, e := range envs {
		for k, v := range e {
			c.Env[k] = v
		}
	}

	return json.NewEncoder(f).Encode(c)
}

func writeHostname(path, hostname string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	pos, err := f.Seek(0, os.SEEK_END)
	if err != nil {
		return err
	}
	if pos > 0 {
		if _, err := f.Write([]byte("\n")); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintf(f, "127.0.0.1 %s\n", hostname)
	return err
}

func readDockerImageConfig(id string) (*dockerImageConfig, error) {
	res := &struct{ Config dockerImageConfig }{}
	f, err := os.Open(filepath.Join(imageRoot, "graph", id, "json"))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if err := json.NewDecoder(f).Decode(res); err != nil {
		return nil, err
	}
	return &res.Config, nil
}

var networkConfigAttempts = attempt.Strategy{
	Total: 10 * time.Minute,
	Delay: 200 * time.Millisecond,
}

// ConfigureNetworking is called once during host startup and passed the
// strategy and identifier of the networking coordinatior job. Currently the
// only strategy implemented uses flannel.
func (l *LibvirtLXCBackend) ConfigureNetworking(strategy NetworkStrategy, job string) (*NetworkInfo, error) {
	if strategy != NetworkStrategyFlannel {
		return nil, errors.New("host: unknown network strategy")
	}

	// wait for the job to start
	func() {
		events := l.state.AddListener(job)
		defer l.state.RemoveListener(job, events)

		job := l.state.GetJob(job)
		if job.Status != host.StatusStarting {
			return
		}

		// job is already created, so the next event will be running, crashed, or failed
		evt := <-events
		switch evt.Event {
		case "start", "stop", "error":
		default:
			panic(fmt.Sprintf("unexpected job event %q", evt.Event))
		}
	}()

	container, err := l.getContainer(job)
	if err != nil {
		return nil, err
	}

	path := filepath.Join(container.RootPath, "/run/flannel/subnet.env")
	var data []byte
	err = networkConfigAttempts.Run(func() error {
		select {
		case <-container.done:
			return errors.New("host: networking container unexpectedly gone")
		default:
		}

		var err error
		data, err = ioutil.ReadFile(path)
		return err
	})
	if err != nil {
		return nil, err
	}

	for _, line := range bytes.Split(data, []byte("\n")) {
		if bytes.HasPrefix(line, []byte("FLANNEL_MTU=")) {
			l.ifaceMTU, err = strconv.Atoi(string(line[12:]))
			if err != nil {
				return nil, fmt.Errorf("host: error parsing mtu %q - %s", string(line), err)
			}
		}
		if bytes.HasPrefix(line, []byte("FLANNEL_SUBNET=")) {
			l.bridgeAddr, l.bridgeNet, err = net.ParseCIDR(string(line[15:]))
			if err != nil {
				return nil, fmt.Errorf("host: error parsing subnet %q - %s", string(line), err)
			}
		}
	}

	if l.ifaceMTU == 0 || l.bridgeAddr == nil || l.bridgeNet == nil {
		return nil, fmt.Errorf("host: error parsing flannel config - %q", string(data))
	}
	l.ipalloc.RequestIP(l.bridgeNet, l.bridgeAddr)

	err = netlink.CreateBridge(bridgeName, false)
	bridgeExists := os.IsExist(err)
	if err != nil && !bridgeExists {
		return nil, err
	}

	bridge, err := net.InterfaceByName(bridgeName)
	if err != nil {
		return nil, err
	}
	if !bridgeExists {
		// We need to explicitly assign the MAC address to avoid it changing to a lower value
		// See: https://github.com/flynn/flynn/issues/223
		b := random.Bytes(5)
		bridgeMAC := fmt.Sprintf("fe:%02x:%02x:%02x:%02x:%02x", b[0], b[1], b[2], b[3], b[4])
		if err := netlink.NetworkSetMacAddress(bridge, bridgeMAC); err != nil {
			return nil, err
		}
	}
	currAddrs, err := bridge.Addrs()
	if err != nil {
		return nil, err
	}
	setIP := true
	for _, addr := range currAddrs {
		ip, net, _ := net.ParseCIDR(addr.String())
		if ip.Equal(l.bridgeAddr) && net.String() == l.bridgeNet.String() {
			setIP = false
		} else {
			if err := netlink.NetworkLinkDelIp(bridge, ip, net); err != nil {
				return nil, err
			}
		}
	}
	if setIP {
		if err := netlink.NetworkLinkAddIp(bridge, l.bridgeAddr, l.bridgeNet); err != nil {
			return nil, err
		}
	}
	if err := netlink.NetworkLinkUp(bridge); err != nil {
		return nil, err
	}

	network, err := l.libvirt.LookupNetworkByName(libvirtNetName)
	if err != nil {
		// network doesn't exist
		networkConfig := &lt.Network{
			Name:    libvirtNetName,
			Bridge:  lt.Bridge{Name: bridgeName},
			Forward: lt.Forward{Mode: "bridge"},
		}
		network, err = l.libvirt.NetworkDefineXML(string(networkConfig.XML()))
		if err != nil {
			return nil, err
		}
	}
	active, err := network.IsActive()
	if err != nil {
		return nil, err
	}
	if !active {
		if err := network.Create(); err != nil {
			return nil, err
		}
	}
	if defaultNet, err := l.libvirt.LookupNetworkByName("default"); err == nil {
		// The default network causes dnsmasq to run and bind to all interfaces,
		// including ours. This prevents discoverd from binding its DNS server.
		// We don't use it, so destroy it if it exists.
		defaultNet.Destroy()
	}

	// enable IP forwarding
	if err := ioutil.WriteFile("/proc/sys/net/ipv4/ip_forward", []byte("1\n"), 0644); err != nil {
		return nil, err
	}

	// Set up iptables for outbound traffic masquerading from containers to the
	// rest of the network.
	if err := iptables.EnableOutboundNAT(bridgeName, l.bridgeNet.String()); err != nil {
		return nil, err
	}

	// Read DNS config, discoverd uses the nameservers
	dnsConf, err := dns.ClientConfigFromFile("/etc/resolv.conf")
	if err != nil {
		return nil, err
	}

	// Write a resolv.conf to be bind-mounted into containers pointing at the
	// future discoverd DNS listener
	if err := os.MkdirAll("/etc/flynn", 0755); err != nil {
		return nil, err
	}
	var resolvSearch string
	if len(dnsConf.Search) > 0 {
		resolvSearch = fmt.Sprintf("search %s\n", strings.Join(dnsConf.Search, " "))
	}
	if err := ioutil.WriteFile("/etc/flynn/resolv.conf", []byte(fmt.Sprintf("%snameserver %s\n", resolvSearch, l.bridgeAddr.String())), 0644); err != nil {
		return nil, err
	}
	l.resolvConf = "/etc/flynn/resolv.conf"

	// Allocate IPs for running jobs
	for i, container := range l.containers {
		if !container.job.Config.HostNetwork {
			var err error
			l.containers[i].IP, err = l.ipalloc.RequestIP(l.bridgeNet, container.IP)
			if err != nil {
				grohl.Log(grohl.Data{"fn": "ConfigureNetworking", "at": "request_ip", "status": "error", "err": err})
			}
		}
	}

	return &NetworkInfo{BridgeAddr: l.bridgeAddr.String(), Nameservers: dnsConf.Servers}, nil
}

var libvirtAttempts = attempt.Strategy{
	Total: 10 * time.Second,
	Delay: 200 * time.Millisecond,
}

func (l *LibvirtLXCBackend) withConnRetries(f func() error) error {
	return libvirtAttempts.Run(func() error {
		err := f()
		if err != nil {
			if alive, err := l.libvirt.IsAlive(); err != nil || !alive {
				conn, connErr := libvirt.NewVirConnection("lxc:///")
				if connErr != nil {
					return connErr
				}
				l.libvirt = conn
			}
		}
		return err
	})
}

func (l *LibvirtLXCBackend) SetDefaultEnv(k, v string) {
	l.envMtx.Lock()
	defer l.envMtx.Unlock()
	l.defaultEnv[k] = v
}

func (l *LibvirtLXCBackend) Run(job *host.Job, runConfig *RunConfig) (err error) {
	g := grohl.NewContext(grohl.Data{"backend": "libvirt-lxc", "fn": "run", "job.id": job.ID})
	g.Log(grohl.Data{"at": "start", "job.artifact.uri": job.Artifact.URI, "job.cmd": job.Config.Cmd})

	if runConfig == nil {
		runConfig = &RunConfig{}
	}
	container := &libvirtContainer{
		l:    l,
		job:  job,
		done: make(chan struct{}),
	}
	if !job.Config.HostNetwork {
		container.IP, err = l.ipalloc.RequestIP(l.bridgeNet, runConfig.IP)
		if err != nil {
			g.Log(grohl.Data{"at": "request_ip", "status": "error", "err": err})
			return err
		}
	}
	defer func() {
		if err != nil {
			go container.cleanup()
		}
	}()

	g.Log(grohl.Data{"at": "pull_image"})
	layers, err := l.pinkertonPull(job.Artifact.URI)
	if err != nil {
		g.Log(grohl.Data{"at": "pull_image", "status": "error", "err": err})
		return err
	}
	imageID, err := pinkerton.ImageID(job.Artifact.URI)
	if err == pinkerton.ErrNoImageID && len(layers) > 0 {
		imageID = layers[len(layers)-1].ID
	} else if err != nil {
		g.Log(grohl.Data{"at": "image_id", "status": "error", "err": err})
		return err
	}

	g.Log(grohl.Data{"at": "read_config"})
	imageConfig, err := readDockerImageConfig(imageID)
	if err != nil {
		g.Log(grohl.Data{"at": "read_config", "status": "error", "err": err})
		return err
	}

	g.Log(grohl.Data{"at": "checkout"})
	rootPath, err := l.pinkerton.Checkout(job.ID, imageID)
	if err != nil {
		g.Log(grohl.Data{"at": "checkout", "status": "error", "err": err})
		return err
	}
	container.RootPath = rootPath

	g.Log(grohl.Data{"at": "mount"})
	if err := bindMount(l.InitPath, filepath.Join(rootPath, ".containerinit"), false, true); err != nil {
		g.Log(grohl.Data{"at": "mount", "file": ".containerinit", "status": "error", "err": err})
		return err
	}
	if err := os.MkdirAll(filepath.Join(rootPath, "etc"), 0755); err != nil {
		g.Log(grohl.Data{"at": "mkdir", "dir": "etc", "status": "error", "err": err})
		return err
	}

	if err := bindMount(l.resolvConf, filepath.Join(rootPath, "etc/resolv.conf"), false, true); err != nil {
		g.Log(grohl.Data{"at": "mount", "file": "resolv.conf", "status": "error", "err": err})
		return err
	}

	if err := writeHostname(filepath.Join(rootPath, "etc/hosts"), job.ID); err != nil {
		g.Log(grohl.Data{"at": "write_hosts", "status": "error", "err": err})
		return err
	}
	if err := os.MkdirAll(filepath.Join(rootPath, ".container-shared"), 0700); err != nil {
		g.Log(grohl.Data{"at": "mkdir", "dir": ".container-shared", "status": "error", "err": err})
		return err
	}
	for i, m := range job.Config.Mounts {
		if err := os.MkdirAll(filepath.Join(rootPath, m.Location), 0755); err != nil {
			g.Log(grohl.Data{"at": "mkdir_mount", "dir": m.Location, "status": "error", "err": err})
			return err
		}
		if m.Target == "" {
			m.Target = filepath.Join(l.VolPath, cluster.RandomJobID(""))
			job.Config.Mounts[i].Target = m.Target
			if err := os.MkdirAll(m.Target, 0755); err != nil {
				g.Log(grohl.Data{"at": "mkdir_vol", "dir": m.Target, "status": "error", "err": err})
				return err
			}
		}
		if err := bindMount(m.Target, filepath.Join(rootPath, m.Location), m.Writeable, true); err != nil {
			g.Log(grohl.Data{"at": "mount", "target": m.Target, "location": m.Location, "status": "error", "err": err})
			return err
		}
	}

	// apply volumes
	for _, v := range job.Config.Volumes {
		vol := l.vman.GetVolume(v.VolumeID)
		if vol == nil {
			err := fmt.Errorf("job %s required volume %s, but that volume does not exist", job.ID, v.VolumeID)
			g.Log(grohl.Data{"at": "volume", "volumeID": v.VolumeID, "status": "error", "err": err})
			return err
		}
		if err := os.MkdirAll(filepath.Join(rootPath, v.Target), 0755); err != nil {
			g.Log(grohl.Data{"at": "volume_mkdir", "dir": v.Target, "status": "error", "err": err})
			return err
		}
		if err != nil {
			g.Log(grohl.Data{"at": "volume_mount", "target": v.Target, "volumeID": v.VolumeID, "status": "error", "err": err})
			return err
		}
		if err := bindMount(vol.Location(), filepath.Join(rootPath, v.Target), v.Writeable, true); err != nil {
			g.Log(grohl.Data{"at": "volume_mount2", "target": v.Target, "volumeID": v.VolumeID, "status": "error", "err": err})
			return err
		}
	}

	if job.Config.Env == nil {
		job.Config.Env = make(map[string]string)
	}
	for i, p := range job.Config.Ports {
		if p.Proto != "tcp" && p.Proto != "udp" {
			return fmt.Errorf("unknown port proto %q", p.Proto)
		}

		if p.Port == 0 {
			job.Config.Ports[i].Port = 5000 + i
		}
		if i == 0 {
			job.Config.Env["PORT"] = strconv.Itoa(job.Config.Ports[i].Port)
		}
		job.Config.Env[fmt.Sprintf("PORT_%d", i)] = strconv.Itoa(job.Config.Ports[i].Port)
	}

	if !job.Config.HostNetwork {
		job.Config.Env["EXTERNAL_IP"] = container.IP.String()
	}

	config := &containerinit.Config{
		TTY:       job.Config.TTY,
		OpenStdin: job.Config.Stdin,
		WorkDir:   job.Config.WorkingDir,
		Resources: job.Resources,
	}
	if !job.Config.HostNetwork {
		config.IP = container.IP.String() + "/24"
		config.Gateway = l.bridgeAddr.String()
	}
	if config.WorkDir == "" {
		config.WorkDir = imageConfig.WorkingDir
	}
	if job.Config.Uid > 0 {
		config.User = strconv.Itoa(job.Config.Uid)
	} else if imageConfig.User != "" {
		// TODO: check and lookup user from image config
	}
	if len(job.Config.Entrypoint) > 0 {
		config.Args = job.Config.Entrypoint
		config.Args = append(config.Args, job.Config.Cmd...)
	} else {
		config.Args = imageConfig.Entrypoint
		if len(job.Config.Cmd) > 0 {
			config.Args = append(config.Args, job.Config.Cmd...)
		} else {
			config.Args = append(config.Args, imageConfig.Cmd...)
		}
	}
	for _, port := range job.Config.Ports {
		config.Ports = append(config.Ports, port)
	}

	g.Log(grohl.Data{"at": "write_config"})
	l.envMtx.RLock()
	err = writeContainerConfig(filepath.Join(rootPath, ".containerconfig"), config,
		map[string]string{
			"PATH": "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
			"TERM": "xterm",
			"HOME": "/",
		},
		l.defaultEnv,
		job.Config.Env,
		map[string]string{
			"HOSTNAME": job.ID,
		},
	)
	l.envMtx.RUnlock()
	if err != nil {
		g.Log(grohl.Data{"at": "write_config", "status": "error", "err": err})
		return err
	}

	l.state.AddJob(job, container.IP)
	domain := &lt.Domain{
		Type:   "lxc",
		Name:   job.ID,
		Memory: lt.UnitInt{Value: 1, Unit: "GiB"},
		OS: lt.OS{
			Type: lt.OSType{Value: "exe"},
			Init: "/.containerinit",
		},
		Devices: lt.Devices{
			Filesystems: []lt.Filesystem{{
				Type:   "mount",
				Source: lt.FSRef{Dir: rootPath},
				Target: lt.FSRef{Dir: "/"},
			}},
			Consoles: []lt.Console{{Type: "pty"}},
		},
		OnPoweroff: "preserve",
		OnCrash:    "preserve",
	}
	if spec, ok := job.Resources[resource.TypeMemory]; ok && spec.Limit != nil {
		domain.Memory = lt.UnitInt{Value: *spec.Limit, Unit: "bytes"}
	}

	if !job.Config.HostNetwork {
		domain.Devices.Interfaces = []lt.Interface{{
			Type:   "network",
			Source: lt.InterfaceSrc{Network: libvirtNetName},
		}}
	}

	// attempt to run libvirt commands multiple times in case the libvirt daemon is
	// temporarily unavailable (e.g. it has restarted, which sometimes happens in CI)
	g.Log(grohl.Data{"at": "define_domain"})
	var vd libvirt.VirDomain
	if err := l.withConnRetries(func() (err error) {
		vd, err = l.libvirt.DomainDefineXML(string(domain.XML()))
		return
	}); err != nil {
		g.Log(grohl.Data{"at": "define_domain", "status": "error", "err": err})
		return err
	}

	g.Log(grohl.Data{"at": "create_domain"})
	if err := l.withConnRetries(vd.Create); err != nil {
		g.Log(grohl.Data{"at": "create_domain", "status": "error", "err": err})
		return err
	}
	uuid, err := vd.GetUUIDString()
	if err != nil {
		g.Log(grohl.Data{"at": "get_domain_uuid", "status": "error", "err": err})
		return err
	}
	g.Log(grohl.Data{"at": "get_uuid", "uuid": uuid})
	l.state.SetContainerID(job.ID, uuid)

	domainXML, err := vd.GetXMLDesc(0)
	if err != nil {
		g.Log(grohl.Data{"at": "get_domain_xml", "status": "error", "err": err})
		return err
	}
	domain = &lt.Domain{}
	if err := xml.Unmarshal([]byte(domainXML), domain); err != nil {
		g.Log(grohl.Data{"at": "unmarshal_domain_xml", "status": "error", "err": err})
		return err
	}

	go container.watch(nil)

	g.Log(grohl.Data{"at": "finish"})
	return nil
}

func (l *LibvirtLXCBackend) openLog(id string) *logbuf.Log {
	l.logsMtx.Lock()
	defer l.logsMtx.Unlock()
	if _, ok := l.logs[id]; !ok {
		// TODO: configure retention and log size
		l.logs[id] = logbuf.NewLog(&lumberjack.Logger{Filename: filepath.Join(l.LogPath, id, id+".log")})
	}
	// TODO: do reference counting and remove logs that are not in use from memory
	return l.logs[id]
}

func (c *libvirtContainer) watch(ready chan<- error) error {
	g := grohl.NewContext(grohl.Data{"backend": "libvirt-lxc", "fn": "watch_container", "job.id": c.job.ID})
	g.Log(grohl.Data{"at": "start"})

	defer func() {
		// TODO: kill containerinit/domain if it is still running
		c.l.containersMtx.Lock()
		delete(c.l.containers, c.job.ID)
		c.l.containersMtx.Unlock()
		c.cleanup()
		close(c.done)
	}()

	var symlinked bool
	var err error
	symlink := "/tmp/containerinit-rpc." + c.job.ID
	socketPath := path.Join(c.RootPath, containerinit.SocketPath)
	for startTime := time.Now(); time.Since(startTime) < 10*time.Second; time.Sleep(time.Millisecond) {
		if !symlinked {
			// We can't connect to the socket file directly because
			// the path to it is longer than 108 characters (UNIX_PATH_MAX).
			// Create a temporary symlink to connect to.
			if err = os.Symlink(socketPath, symlink); err != nil && !os.IsExist(err) {
				g.Log(grohl.Data{"at": "symlink_socket", "status": "error", "err": err, "source": socketPath, "target": symlink})
				continue
			}
			defer os.Remove(symlink)
			symlinked = true
		}

		c.Client, err = containerinit.NewClient(symlink)
		if err == nil {
			break
		}
	}
	if ready != nil {
		ready <- err
	}
	if err != nil {
		g.Log(grohl.Data{"at": "connect", "status": "error", "err": err.Error()})
		c.l.state.SetStatusFailed(c.job.ID, errors.New("failed to connect to container"))

		d, e := c.l.libvirt.LookupDomainByName(c.job.ID)
		if e != nil {
			return e
		}
		if err := d.Destroy(); err != nil {
			g.Log(grohl.Data{"at": "destroy", "status": "error", "err": err.Error()})
		}
		return err
	}
	defer c.Client.Close()

	c.l.containersMtx.Lock()
	c.l.containers[c.job.ID] = c
	c.l.containersMtx.Unlock()

	if !c.job.Config.DisableLog && !c.job.Config.TTY {
		g.Log(grohl.Data{"at": "get_stdout"})
		stdout, stderr, initLog, err := c.Client.GetStreams()
		if err != nil {
			g.Log(grohl.Data{"at": "get_streams", "status": "error", "err": err.Error()})
			return err
		}

		log := c.l.openLog(c.job.ID)
		defer log.Close()

		muxConfig := logmux.Config{
			AppID:   c.job.Metadata["flynn-controller.app"],
			HostID:  c.l.state.id,
			JobType: c.job.Metadata["flynn-controller.type"],
			JobID:   c.job.ID,
		}

		// TODO(benburkert): remove file logging once attach proto uses logaggregator
		streams := []io.Reader{stdout, stderr}
		for i, stream := range streams {
			bufr, bufw := io.Pipe()
			muxr, muxw := io.Pipe()
			go func(r io.Reader, pw1, pw2 *io.PipeWriter) {
				mw := io.MultiWriter(pw1, pw2)
				_, err := io.Copy(mw, r)
				pw1.CloseWithError(err)
				pw2.CloseWithError(err)
			}(stream, bufw, muxw)

			fd := i + 1
			go log.Follow(fd, bufr)
			go c.l.mux.Follow(muxr, fd, muxConfig)
		}

		go log.Follow(3, initLog)
	}

	g.Log(grohl.Data{"at": "watch_changes"})
	for change := range c.Client.StreamState() {
		g.Log(grohl.Data{"at": "change", "state": change.State.String()})
		if change.Error != "" {
			err := errors.New(change.Error)
			g.Log(grohl.Data{"at": "change", "status": "error", "err": err})
			c.Client.Resume()
			c.l.state.SetStatusFailed(c.job.ID, err)
			return err
		}
		switch change.State {
		case containerinit.StateInitial:
			g.Log(grohl.Data{"at": "wait_attach"})
			c.l.state.WaitAttach(c.job.ID)
			g.Log(grohl.Data{"at": "resume"})
			c.Client.Resume()
		case containerinit.StateRunning:
			g.Log(grohl.Data{"at": "running"})
			c.l.state.SetStatusRunning(c.job.ID)

			// if the job was stopped before it started, exit
			if c.l.state.GetJob(c.job.ID).ForceStop {
				c.Stop()
			}
		case containerinit.StateExited:
			g.Log(grohl.Data{"at": "exited", "status": change.ExitStatus})
			c.Client.Resume()
			c.l.state.SetStatusDone(c.job.ID, change.ExitStatus)
			return nil
		case containerinit.StateFailed:
			g.Log(grohl.Data{"at": "failed"})
			c.Client.Resume()
			c.l.state.SetStatusFailed(c.job.ID, errors.New("container failed to start"))
			return nil
		}
	}
	g.Log(grohl.Data{"at": "unknown_failure"})
	c.l.state.SetStatusFailed(c.job.ID, errors.New("unknown failure"))

	return nil
}

func (c *libvirtContainer) cleanup() error {
	g := grohl.NewContext(grohl.Data{"backend": "libvirt-lxc", "fn": "cleanup", "job.id": c.job.ID})
	g.Log(grohl.Data{"at": "start"})

	if err := syscall.Unmount(filepath.Join(c.RootPath, ".containerinit"), 0); err != nil {
		g.Log(grohl.Data{"at": "unmount", "file": ".containerinit", "status": "error", "err": err})
	}
	if err := syscall.Unmount(filepath.Join(c.RootPath, "etc/resolv.conf"), 0); err != nil {
		g.Log(grohl.Data{"at": "unmount", "file": "resolv.conf", "status": "error", "err": err})
	}
	if err := c.l.pinkerton.Cleanup(c.job.ID); err != nil {
		g.Log(grohl.Data{"at": "pinkerton", "status": "error", "err": err})
	}
	for _, m := range c.job.Config.Mounts {
		if err := syscall.Unmount(filepath.Join(c.RootPath, m.Location), 0); err != nil {
			g.Log(grohl.Data{"at": "unmount", "location": m.Location, "status": "error", "err": err})
		}
	}
	for _, v := range c.job.Config.Volumes {
		if err := syscall.Unmount(filepath.Join(c.RootPath, v.Target), 0); err != nil {
			g.Log(grohl.Data{"at": "unmount", "target": v.Target, "volumeID": v.VolumeID, "status": "error", "err": err})
		}
	}
	if !c.job.Config.HostNetwork && c.l.bridgeNet != nil {
		c.l.ipalloc.ReleaseIP(c.l.bridgeNet, c.IP)
	}
	g.Log(grohl.Data{"at": "finish"})
	return nil
}

func (c *libvirtContainer) WaitStop(timeout time.Duration) error {
	job := c.l.state.GetJob(c.job.ID)
	if job.Status == host.StatusDone || job.Status == host.StatusFailed {
		return nil
	}
	select {
	case <-c.done:
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("Timed out: %v", timeout)
	}
}

func (c *libvirtContainer) Stop() error {
	if err := c.Signal(int(syscall.SIGTERM)); err != nil {
		return err
	}
	if err := c.WaitStop(10 * time.Second); err != nil {
		return c.Signal(int(syscall.SIGKILL))
	}
	return nil
}

func (l *LibvirtLXCBackend) Stop(id string) error {
	c, err := l.getContainer(id)
	if err != nil {
		return err
	}
	return c.Stop()
}

func (l *LibvirtLXCBackend) getContainer(id string) (*libvirtContainer, error) {
	l.containersMtx.RLock()
	defer l.containersMtx.RUnlock()
	c := l.containers[id]
	if c == nil {
		return nil, errors.New("libvirt: unknown container")
	}
	return c, nil
}

func (l *LibvirtLXCBackend) ResizeTTY(id string, height, width uint16) error {
	container, err := l.getContainer(id)
	if err != nil {
		return err
	}
	if !container.job.Config.TTY {
		return errors.New("job doesn't have a TTY")
	}
	pty, err := container.GetPtyMaster()
	if err != nil {
		return err
	}
	return term.SetWinsize(pty.Fd(), &term.Winsize{Height: height, Width: width})
}

func (l *LibvirtLXCBackend) Signal(id string, sig int) error {
	container, err := l.getContainer(id)
	if err != nil {
		return err
	}
	return container.Signal(sig)
}

func (l *LibvirtLXCBackend) Attach(req *AttachRequest) (err error) {
	client, err := l.getContainer(req.Job.Job.ID)
	if err != nil && (req.Job.Job.Config.TTY || req.Stdin != nil) {
		return err
	}

	defer func() {
		if client != nil && (req.Job.Job.Config.TTY || req.Stream) && err == io.EOF {
			<-client.done
			job := l.state.GetJob(req.Job.Job.ID)
			if job.Status == host.StatusDone || job.Status == host.StatusCrashed {
				err = ExitError(job.ExitStatus)
				return
			}
			err = errors.New(*job.Error)
		}
	}()

	if req.Job.Job.Config.TTY {
		pty, err := client.GetPtyMaster()
		if err != nil {
			return err
		}
		if err := term.SetWinsize(pty.Fd(), &term.Winsize{Height: req.Height, Width: req.Width}); err != nil {
			return err
		}
		if req.Attached != nil {
			req.Attached <- struct{}{}
		}
		if req.Stdin != nil && req.Stdout != nil {
			go io.Copy(pty, req.Stdin)
		} else if req.Stdin != nil {
			io.Copy(pty, req.Stdin)
		}
		if req.Stdout != nil {
			io.Copy(req.Stdout, pty)
		}
		pty.Close()
		return io.EOF
	}
	if req.Stdin != nil {
		stdinPipe, err := client.GetStdin()
		if err != nil {
			return err
		}
		go func() {
			io.Copy(stdinPipe, req.Stdin)
			stdinPipe.Close()
		}()
	}

	if req.Job.Job.Config.DisableLog {
		stdout, stderr, initLog, err := client.GetStreams()
		if err != nil {
			return err
		}
		if req.Attached != nil {
			req.Attached <- struct{}{}
		}
		var wg sync.WaitGroup
		cp := func(w io.Writer, r io.Reader) {
			if w == nil {
				w = ioutil.Discard
			}
			wg.Add(1)
			go func() {
				io.Copy(w, r)
				wg.Done()
			}()
		}
		cp(req.InitLog, initLog)
		cp(req.Stdout, stdout)
		cp(req.Stderr, stderr)
		wg.Wait()
		return io.EOF
	}

	if req.Attached != nil {
		req.Attached <- struct{}{}
	}

	lines := -1
	if !req.Logs {
		lines = 0
	}

	log := l.openLog(req.Job.Job.ID)
	ch := make(chan logbuf.Data)
	done := make(chan struct{})
	go log.Read(lines, req.Stream, ch, done)
	defer close(done)

	for data := range ch {
		var w io.Writer
		switch data.Stream {
		case 1:
			w = req.Stdout
		case 2:
			w = req.Stderr
		case 3:
			w = req.InitLog
		}
		if w == nil {
			continue
		}
		if _, err := w.Write([]byte(data.Message)); err != nil {
			return nil
		}
	}

	return io.EOF
}

func (l *LibvirtLXCBackend) Cleanup() error {
	g := grohl.NewContext(grohl.Data{"backend": "libvirt-lxc", "fn": "Cleanup"})
	l.containersMtx.Lock()
	ids := make([]string, 0, len(l.containers))
	for id := range l.containers {
		ids = append(ids, id)
	}
	l.containersMtx.Unlock()
	g.Log(grohl.Data{"at": "start", "count": len(ids)})
	errs := make(chan error)
	for _, id := range ids {
		go func(id string) {
			g.Log(grohl.Data{"at": "stop", "job.id": id})
			err := l.Stop(id)
			if err != nil {
				g.Log(grohl.Data{"at": "error", "job.id": id, "err": err.Error()})
			}
			errs <- err
		}(id)
	}
	var err error
	for i := 0; i < len(ids); i++ {
		stopErr := <-errs
		if stopErr != nil {
			err = stopErr
		}
	}
	g.Log(grohl.Data{"at": "finish"})
	return err
}

/*
	Loads a series of jobs, and reconstructs whatever additional backend state was saved.

	This may include reconnecting rpc systems and communicating with containers
	(thus this may take a significant moment; it's not just deserializing).
*/
func (l *LibvirtLXCBackend) UnmarshalState(jobs map[string]*host.ActiveJob, jobBackendStates map[string][]byte, backendGlobalState []byte) error {
	containers := make(map[string]*libvirtContainer)
	for k, v := range jobBackendStates {
		container := &libvirtContainer{}
		if err := json.Unmarshal(v, container); err != nil {
			return fmt.Errorf("failed to deserialize backed container state: %s", err)
		}
		containers[k] = container
	}
	readySignals := make(map[string]chan error)
	// for every job with a matching container, attempt to restablish a connection
	for _, j := range jobs {
		container, ok := containers[j.Job.ID]
		if !ok {
			continue
		}
		container.l = l
		container.job = j.Job
		container.done = make(chan struct{})
		readySignals[j.Job.ID] = make(chan error)
		go container.watch(readySignals[j.Job.ID])
	}
	// gather connection attempts and finish reconstruction if success.  failures will time out.
	for _, j := range jobs {
		container, ok := containers[j.Job.ID]
		if !ok {
			continue
		}
		if err := <-readySignals[j.Job.ID]; err != nil {
			// log error
			l.state.RemoveJob(j.Job.ID)
			container.cleanup()
			continue
		}
		l.containers[j.Job.ID] = container
	}
	return nil
}

func (l *LibvirtLXCBackend) MarshalJobState(jobID string) ([]byte, error) {
	l.containersMtx.RLock()
	defer l.containersMtx.RUnlock()
	if associatedState, exists := l.containers[jobID]; exists {
		return json.Marshal(associatedState)
	}
	return nil, nil
}

func (l *LibvirtLXCBackend) pinkertonPull(url string) ([]layer.PullInfo, error) {
	var layers []layer.PullInfo
	info := make(chan layer.PullInfo)
	done := make(chan struct{})
	go func() {
		for l := range info {
			layers = append(layers, l)
		}
		close(done)
	}()
	if err := l.pinkerton.PullDocker(url, info); err != nil {
		return nil, err
	}
	<-done
	return layers, nil
}

func bindMount(src, dest string, writeable, private bool) error {
	srcStat, err := os.Stat(src)
	if err != nil {
		return err
	}
	if _, err := os.Stat(dest); os.IsNotExist(err) {
		if srcStat.IsDir() {
			if err := os.MkdirAll(dest, 0755); err != nil {
				return err
			}
		} else {
			if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
				return err
			}
			f, err := os.OpenFile(dest, os.O_CREATE, 0755)
			if err != nil {
				return err
			}
			f.Close()
		}
	} else if err != nil {
		return err
	}

	flags := syscall.MS_BIND | syscall.MS_REC
	if !writeable {
		flags |= syscall.MS_RDONLY
	}

	if err := syscall.Mount(src, dest, "bind", uintptr(flags), ""); err != nil {
		return err
	}
	if private {
		if err := syscall.Mount("", dest, "none", uintptr(syscall.MS_PRIVATE), ""); err != nil {
			return err
		}
	}
	return nil
}
