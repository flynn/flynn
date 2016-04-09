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
	"os/exec"
	"path"
	"path/filepath"
	"sort"
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
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/technoweenie/grohl"
	"github.com/flynn/flynn/host/containerinit"
	lt "github.com/flynn/flynn/host/libvirt"
	"github.com/flynn/flynn/host/logmux"
	"github.com/flynn/flynn/host/resource"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/host/volume/manager"
	"github.com/flynn/flynn/pinkerton"
	"github.com/flynn/flynn/pinkerton/layer"
	"github.com/flynn/flynn/pkg/attempt"
	"github.com/flynn/flynn/pkg/iptables"
	"github.com/flynn/flynn/pkg/mounts"
	"github.com/flynn/flynn/pkg/random"
	"github.com/flynn/flynn/pkg/syslog/rfc5424"
)

const (
	imageRoot        = "/var/lib/docker"
	flynnRoot        = "/var/lib/flynn"
	defaultPartition = "user"
)

func NewLibvirtLXCBackend(state *State, vman *volumemanager.Manager, bridgeName, initPath, umountPath string, mux *logmux.Mux, partitionCGroups map[string]int64) (Backend, error) {
	libvirtc, err := libvirt.NewVirConnection("lxc:///")
	if err != nil {
		return nil, err
	}

	pinkertonCtx, err := pinkerton.BuildContext("aufs", imageRoot)
	if err != nil {
		return nil, err
	}

	for name, shares := range partitionCGroups {
		if err := createCGroupPartition(name, shares); err != nil {
			return nil, err
		}
	}

	return &LibvirtLXCBackend{
		InitPath:            initPath,
		UmountPath:          umountPath,
		libvirt:             libvirtc,
		state:               state,
		vman:                vman,
		pinkerton:           pinkertonCtx,
		logStreams:          make(map[string]map[string]*logmux.LogStream),
		containers:          make(map[string]*libvirtContainer),
		defaultEnv:          make(map[string]string),
		resolvConf:          "/etc/resolv.conf",
		mux:                 mux,
		ipalloc:             ipallocator.New(),
		bridgeName:          bridgeName,
		discoverdConfigured: make(chan struct{}),
		networkConfigured:   make(chan struct{}),
		partitionCGroups:    partitionCGroups,
	}, nil
}

type LibvirtLXCBackend struct {
	InitPath   string
	UmountPath string
	libvirt    libvirt.VirConnection
	state      *State
	vman       *volumemanager.Manager
	pinkerton  *pinkerton.Context
	ipalloc    *ipallocator.IPAllocator

	ifaceMTU   int
	bridgeName string
	bridgeAddr net.IP
	bridgeNet  *net.IPNet
	resolvConf string

	logStreamMtx sync.Mutex
	logStreams   map[string]map[string]*logmux.LogStream
	mux          *logmux.Mux

	containersMtx sync.RWMutex
	containers    map[string]*libvirtContainer

	envMtx     sync.RWMutex
	defaultEnv map[string]string

	discoverdConfigured chan struct{}
	networkConfigured   chan struct{}

	partitionCGroups map[string]int64 // name -> cpu shares
}

type libvirtContainer struct {
	RootPath string
	Domain   *lt.Domain
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
	_, err = fmt.Fprintf(f, "127.0.0.1 localhost %s\n", hostname)
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
func (l *LibvirtLXCBackend) ConfigureNetworking(config *host.NetworkConfig) error {
	var err error
	l.bridgeAddr, l.bridgeNet, err = net.ParseCIDR(config.Subnet)
	if err != nil {
		return err
	}
	l.ipalloc.RequestIP(l.bridgeNet, l.bridgeAddr)

	err = netlink.CreateBridge(l.bridgeName, false)
	bridgeExists := os.IsExist(err)
	if err != nil && !bridgeExists {
		return err
	}

	bridge, err := net.InterfaceByName(l.bridgeName)
	if err != nil {
		return err
	}
	if !bridgeExists {
		// We need to explicitly assign the MAC address to avoid it changing to a lower value
		// See: https://github.com/flynn/flynn/issues/223
		b := random.Bytes(5)
		bridgeMAC := fmt.Sprintf("fe:%02x:%02x:%02x:%02x:%02x", b[0], b[1], b[2], b[3], b[4])
		if err := netlink.NetworkSetMacAddress(bridge, bridgeMAC); err != nil {
			return err
		}
	}
	currAddrs, err := bridge.Addrs()
	if err != nil {
		return err
	}
	setIP := true
	for _, addr := range currAddrs {
		ip, net, _ := net.ParseCIDR(addr.String())
		if ip.Equal(l.bridgeAddr) && net.String() == l.bridgeNet.String() {
			setIP = false
		} else {
			if err := netlink.NetworkLinkDelIp(bridge, ip, net); err != nil {
				return err
			}
		}
	}
	if setIP {
		if err := netlink.NetworkLinkAddIp(bridge, l.bridgeAddr, l.bridgeNet); err != nil {
			return err
		}
	}
	if err := netlink.NetworkLinkUp(bridge); err != nil {
		return err
	}

	network, err := l.libvirt.LookupNetworkByName(l.bridgeName)
	if err != nil {
		// network doesn't exist
		networkConfig := &lt.Network{
			Name:    l.bridgeName,
			Bridge:  lt.Bridge{Name: l.bridgeName},
			Forward: lt.Forward{Mode: "bridge"},
		}
		network, err = l.libvirt.NetworkDefineXML(string(networkConfig.XML()))
		if err != nil {
			return err
		}
	}
	defer network.Free()
	active, err := network.IsActive()
	if err != nil {
		return err
	}
	if !active {
		if err := network.Create(); err != nil {
			return err
		}
	}
	if defaultNet, err := l.libvirt.LookupNetworkByName("default"); err == nil {
		// The default network causes dnsmasq to run and bind to all interfaces,
		// including ours. This prevents discoverd from binding its DNS server.
		// We don't use it, so destroy it if it exists.
		defaultNet.Destroy()
		defaultNet.Free()
	}

	// enable IP forwarding
	if err := ioutil.WriteFile("/proc/sys/net/ipv4/ip_forward", []byte("1\n"), 0644); err != nil {
		return err
	}

	// Set up iptables for outbound traffic masquerading from containers to the
	// rest of the network.
	if err := iptables.EnableOutboundNAT(l.bridgeName, l.bridgeNet.String()); err != nil {
		return err
	}

	// Read DNS config, discoverd uses the nameservers
	dnsConf, err := dns.ClientConfigFromFile("/etc/resolv.conf")
	if err != nil {
		return err
	}
	config.Resolvers = dnsConf.Servers

	// Write a resolv.conf to be bind-mounted into containers pointing at the
	// future discoverd DNS listener
	if err := os.MkdirAll("/etc/flynn", 0755); err != nil {
		return err
	}
	var resolvSearch string
	if len(dnsConf.Search) > 0 {
		resolvSearch = fmt.Sprintf("search %s\n", strings.Join(dnsConf.Search, " "))
	}
	if err := ioutil.WriteFile("/etc/flynn/resolv.conf", []byte(fmt.Sprintf("%snameserver %s\n", resolvSearch, l.bridgeAddr.String())), 0644); err != nil {
		return err
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

	close(l.networkConfigured)

	return nil
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
	l.defaultEnv[k] = v
	l.envMtx.Unlock()
	if k == "DISCOVERD" {
		close(l.discoverdConfigured)
	}
}

func (l *LibvirtLXCBackend) Run(job *host.Job, runConfig *RunConfig) (err error) {
	g := grohl.NewContext(grohl.Data{"backend": "libvirt-lxc", "fn": "run", "job.id": job.ID})
	g.Log(grohl.Data{"at": "start", "job.artifact.uri": job.ImageArtifact.URI, "job.cmd": job.Config.Cmd})

	defer func() {
		if err != nil {
			l.state.SetStatusFailed(job.ID, err)
		}
	}()

	if job.Partition == "" {
		job.Partition = defaultPartition
	}
	if _, ok := l.partitionCGroups[job.Partition]; !ok {
		return fmt.Errorf("host: invalid job partition %q", job.Partition)
	}

	if !job.Config.HostNetwork {
		<-l.networkConfigured
	}
	if _, ok := job.Config.Env["DISCOVERD"]; !ok {
		<-l.discoverdConfigured
	}

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
		l.state.SetContainerIP(job.ID, container.IP)
	}
	defer func() {
		if err != nil {
			go container.cleanup()
		}
	}()

	g.Log(grohl.Data{"at": "pull_image"})
	layers, err := l.pinkertonPull(job.ImageArtifact.URI)
	if err != nil {
		g.Log(grohl.Data{"at": "pull_image", "status": "error", "err": err})
		return err
	}
	imageID, err := pinkerton.ImageID(job.ImageArtifact.URI)
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
	var rootPath string
	// creating an AUFS mount can fail intermittently with EINVAL, so try a
	// few times (see https://github.com/flynn/flynn/issues/2044)
	for start := time.Now(); time.Since(start) < time.Second; time.Sleep(50 * time.Millisecond) {
		rootPath, err = l.pinkerton.Checkout(job.ID, imageID)
		if err == nil || !strings.HasSuffix(err.Error(), "invalid argument") {
			break
		}
	}
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

	jobIDParts := strings.SplitN(job.ID, "-", 2)
	var hostname string
	if len(jobIDParts) == 1 {
		hostname = jobIDParts[0]
	} else {
		hostname = jobIDParts[1]
	}
	if len(hostname) > 64 {
		hostname = hostname[:64]
	}

	if err := writeHostname(filepath.Join(rootPath, "etc/hosts"), hostname); err != nil {
		g.Log(grohl.Data{"at": "write_hosts", "status": "error", "err": err})
		return err
	}
	if err := os.MkdirAll(filepath.Join(rootPath, ".container-shared"), 0700); err != nil {
		g.Log(grohl.Data{"at": "mkdir", "dir": ".container-shared", "status": "error", "err": err})
		return err
	}
	for _, m := range job.Config.Mounts {
		if err := os.MkdirAll(filepath.Join(rootPath, m.Location), 0755); err != nil {
			g.Log(grohl.Data{"at": "mkdir_mount", "dir": m.Location, "status": "error", "err": err})
			return err
		}
		if m.Target == "" {
			return errors.New("host: invalid empty mount target")
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
			err := fmt.Errorf("unknown port proto %q", p.Proto)
			g.Log(grohl.Data{"at": "alloc_port", "proto": p.Proto, "status": "error", "err": err})
			return err
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
			"HOSTNAME": hostname,
		},
	)
	l.envMtx.RUnlock()
	if err != nil {
		g.Log(grohl.Data{"at": "write_config", "status": "error", "err": err})
		return err
	}

	domain := &lt.Domain{
		Type:   "lxc",
		Name:   job.ID,
		Memory: lt.UnitInt{Value: 1, Unit: "GiB"},
		OS: lt.OS{
			Type: lt.OSType{Value: "exe"},
			Init: "/.containerinit",
		},
		Devices: lt.Devices{
			Filesystems: []lt.Filesystem{
				{
					Type:   "mount",
					Source: lt.FSRef{Dir: rootPath},
					Target: lt.FSRef{Dir: "/"},
				},
				{
					Type:   "ram",
					Source: lt.FSRef{Usage: "65535"}, // 64MiB
					Target: lt.FSRef{Dir: "/dev/shm"},
				},
			},
			Consoles: []lt.Console{{Type: "pty"}},
		},
		Resource: &lt.Resource{
			Partition: "/machine/" + job.Partition,
		},
		OnPoweroff: "preserve",
		OnCrash:    "preserve",
	}
	if spec, ok := job.Resources[resource.TypeMemory]; ok && spec.Limit != nil {
		domain.Memory = lt.UnitInt{Value: *spec.Limit, Unit: "bytes"}
	}
	if spec, ok := job.Resources[resource.TypeCPU]; ok && spec.Limit != nil {
		domain.CPUTune = &lt.CPUTune{Shares: milliCPUToShares(*spec.Limit)}
	}

	if !job.Config.HostNetwork {
		domain.Devices.Interfaces = []lt.Interface{{
			Type:   "network",
			Source: lt.InterfaceSrc{Network: l.bridgeName},
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
	defer vd.Free()

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
	container.Domain = &lt.Domain{}
	if err := xml.Unmarshal([]byte(domainXML), container.Domain); err != nil {
		g.Log(grohl.Data{"at": "unmarshal_domain_xml", "status": "error", "err": err})
		return err
	}

	go container.watch(nil, nil)

	g.Log(grohl.Data{"at": "finish"})
	return nil
}

func (c *libvirtContainer) cleanupMounts(pid int) error {
	list, err := mounts.ParseFile(fmt.Sprintf("/proc/%d/mounts", pid))
	if err != nil {
		return err
	}
	sort.Sort(mounts.ByDepth(list))

	args := make([]string, 1, len(list)+1)
	args[0] = strconv.Itoa(pid)
	for _, m := range list {
		if strings.HasPrefix(m.Mountpoint, imageRoot) || strings.HasPrefix(m.Mountpoint, flynnRoot) {
			args = append(args, m.Mountpoint)
		}
	}
	if len(args) <= 1 {
		// no mountpoints to clean up
		return nil
	}

	out, err := exec.Command(c.l.UmountPath, args...).CombinedOutput()
	if err != nil {
		desc := err.Error()
		if len(out) > 0 {
			desc = string(out)
		}
		return fmt.Errorf("host: error running nsumount %d: %s", pid, desc)
	}
	return nil
}

// waitExit waits for the libvirt domain to be marked as done or five seconds to
// elapse
func (c *libvirtContainer) waitExit() {
	g := grohl.NewContext(grohl.Data{"backend": "libvirt-lxc", "fn": "waitExit", "job.id": c.job.ID})
	g.Log(grohl.Data{"at": "start"})
	domain, err := c.l.libvirt.LookupDomainByName(c.job.ID)
	if err != nil {
		g.Log(grohl.Data{"at": "domain_error", "err": err.Error()})
		return
	}
	defer domain.Free()

	maxWait := time.After(5 * time.Second)
	for {
		state, err := domain.GetState()
		if err != nil {
			g.Log(grohl.Data{"at": "state_error", "err": err.Error()})
			return
		}
		if state[0] != libvirt.VIR_DOMAIN_RUNNING && state[0] != libvirt.VIR_DOMAIN_SHUTDOWN {
			g.Log(grohl.Data{"at": "done"})
			return
		}
		select {
		case <-maxWait:
			g.Log(grohl.Data{"at": "maxWait"})
			return
		default:
			time.Sleep(100 * time.Millisecond)
		}
	}
}

func (c *libvirtContainer) watch(ready chan<- error, buffer host.LogBuffer) error {
	g := grohl.NewContext(grohl.Data{"backend": "libvirt-lxc", "fn": "watch_container", "job.id": c.job.ID})
	g.Log(grohl.Data{"at": "start"})

	defer func() {
		c.waitExit()
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
		defer d.Free()
		if err := d.Destroy(); err != nil {
			g.Log(grohl.Data{"at": "destroy", "status": "error", "err": err.Error()})
		}
		return err
	}
	defer c.Client.Close()

	go func() {
		// Workaround for mounts leaking into the libvirt_lxc supervisor process,
		// see https://github.com/flynn/flynn/issues/1125 for details. Remove
		// nsumount from the tree when deleting.
		g.Log(grohl.Data{"at": "cleanup_mounts", "status": "start"})
		if err := c.cleanupMounts(c.Domain.ID); err != nil {
			g.Log(grohl.Data{"at": "cleanup_mounts", "status": "error", "err": err})
		}

		// The bind mounts are copied when we spin up the container, we don't
		// need them in the root mount namespace any more.
		c.unbindMounts()
		g.Log(grohl.Data{"at": "cleanup_mounts", "status": "finish"})
	}()

	c.l.containersMtx.Lock()
	c.l.containers[c.job.ID] = c
	c.l.containersMtx.Unlock()

	if !c.job.Config.DisableLog && !c.job.Config.TTY {
		if err := c.followLogs(g, buffer); err != nil {
			return err
		}
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
			g.Log(grohl.Data{"at": "resumed"})
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

func (c *libvirtContainer) followLogs(g *grohl.Context, buffer host.LogBuffer) error {
	c.l.logStreamMtx.Lock()
	defer c.l.logStreamMtx.Unlock()
	if _, ok := c.l.logStreams[c.job.ID]; ok {
		return nil
	}

	g.Log(grohl.Data{"at": "get_stdout"})
	stdout, stderr, initLog, err := c.Client.GetStreams()
	if err != nil {
		g.Log(grohl.Data{"at": "get_streams", "status": "error", "err": err.Error()})
		return err
	}

	nonblocking := func(file *os.File) (net.Conn, error) {
		// convert to a net.Conn so we do non-blocking I/O on the fd and Close
		// will make calls to Read return straight away (using read(2) would
		// not have this same behaviour, meaning we could potentially read
		// from the stream after we have closed and returned the buffer).
		defer file.Close()
		return net.FileConn(file)
	}

	muxConfig := logmux.Config{
		AppID:   c.job.Metadata["flynn-controller.app"],
		HostID:  c.l.state.id,
		JobType: c.job.Metadata["flynn-controller.type"],
		JobID:   c.job.ID,
	}

	logStreams := make(map[string]*logmux.LogStream, 3)
	stdoutR, err := nonblocking(stdout)
	if err != nil {
		g.Log(grohl.Data{"at": "log_stream", "type": "stdout", "status": "error", "err": err.Error()})
		return err
	}
	logStreams["stdout"] = c.l.mux.Follow(stdoutR, buffer["stdout"], 1, muxConfig)

	stderrR, err := nonblocking(stderr)
	if err != nil {
		g.Log(grohl.Data{"at": "log_stream", "type": "stderr", "status": "error", "err": err.Error()})
		return err
	}
	logStreams["stderr"] = c.l.mux.Follow(stderrR, buffer["stderr"], 2, muxConfig)

	initLogR, err := nonblocking(initLog)
	if err != nil {
		g.Log(grohl.Data{"at": "log_stream", "type": "initLog", "status": "error", "err": err.Error()})
		return err
	}
	logStreams["initLog"] = c.l.mux.Follow(initLogR, buffer["initLog"], 3, muxConfig)
	c.l.logStreams[c.job.ID] = logStreams

	return nil
}

func (c *libvirtContainer) unbindMounts() {
	g := grohl.NewContext(grohl.Data{"backend": "libvirt-lxc", "fn": "unbind_mounts", "job.id": c.job.ID})
	g.Log(grohl.Data{"at": "start"})

	if err := syscall.Unmount(filepath.Join(c.RootPath, ".containerinit"), 0); err != nil {
		g.Log(grohl.Data{"at": "unmount", "file": ".containerinit", "status": "error", "err": err})
	}
	if err := syscall.Unmount(filepath.Join(c.RootPath, "etc/resolv.conf"), 0); err != nil {
		g.Log(grohl.Data{"at": "unmount", "file": "resolv.conf", "status": "error", "err": err})
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

	g.Log(grohl.Data{"at": "finish"})
}

func (c *libvirtContainer) cleanup() error {
	g := grohl.NewContext(grohl.Data{"backend": "libvirt-lxc", "fn": "cleanup", "job.id": c.job.ID})
	g.Log(grohl.Data{"at": "start"})

	c.l.logStreamMtx.Lock()
	for _, s := range c.l.logStreams[c.job.ID] {
		s.Close()
	}
	delete(c.l.logStreams, c.job.ID)
	c.l.logStreamMtx.Unlock()

	c.unbindMounts()
	if err := c.l.pinkerton.Cleanup(c.job.ID); err != nil {
		g.Log(grohl.Data{"at": "pinkerton", "status": "error", "err": err})
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
	if err != nil {
		if req.Job.Job.Config.TTY || req.Stdin != nil {
			return host.ErrJobNotRunning
		}

		// if the container has exited and logging was disabled, return EOF
		if req.Job.Job.Config.DisableLog {
			if req.Attached != nil {
				req.Attached <- struct{}{}
			}
			return io.EOF
		}
	}

	defer func() {
		if client != nil && (req.Job.Job.Config.TTY || req.Stream) && err == io.EOF {
			<-client.done
			job := l.state.GetJob(req.Job.Job.ID)
			if job.Status == host.StatusDone || job.Status == host.StatusCrashed {
				err = ExitError(*job.ExitStatus)
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

	ch := make(chan *rfc5424.Message)
	stream, err := l.mux.StreamLog(req.Job.Job.Metadata["flynn-controller.app"], req.Job.Job.ID, req.Logs, req.Stream, ch)
	if err != nil {
		return err
	}
	defer stream.Close()

	for msg := range ch {
		var w io.Writer
		switch string(msg.MsgID) {
		case "ID1":
			w = req.Stdout
		case "ID2":
			w = req.Stderr
		case "ID3":
			w = req.InitLog
		}
		if w == nil {
			continue
		}
		if _, err := w.Write(append(msg.Msg, '\n')); err != nil {
			return nil
		}
	}

	return io.EOF
}

func (l *LibvirtLXCBackend) Cleanup(except []string) error {
	g := grohl.NewContext(grohl.Data{"backend": "libvirt-lxc", "fn": "Cleanup"})
	shouldSkip := func(id string) bool {
		for _, s := range except {
			if id == s {
				return true
			}
		}
		return false
	}
	l.containersMtx.Lock()
	ids := make([]string, 0, len(l.containers))
	for id := range l.containers {
		if shouldSkip(id) {
			continue
		}
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
func (l *LibvirtLXCBackend) UnmarshalState(jobs map[string]*host.ActiveJob, jobBackendStates map[string][]byte, backendGlobalState []byte, buffers host.LogBuffers) error {
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
		go container.watch(readySignals[j.Job.ID], buffers[j.Job.ID])
	}
	// gather connection attempts and finish reconstruction if success.  failures will time out.
	for _, j := range jobs {
		container, ok := containers[j.Job.ID]
		if !ok {
			continue
		}
		if err := <-readySignals[j.Job.ID]; err != nil {
			// log error
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

func (l *LibvirtLXCBackend) OpenLogs(buffers host.LogBuffers) error {
	l.containersMtx.RLock()
	defer l.containersMtx.RUnlock()
	for id, c := range l.containers {
		g := grohl.NewContext(grohl.Data{"backend": "libvirt-lxc", "fn": "OpenLogs", "job.id": id})
		if err := c.followLogs(g, buffers[id]); err != nil {
			return err
		}
	}
	return nil
}

func (l *LibvirtLXCBackend) CloseLogs() (host.LogBuffers, error) {
	g := grohl.NewContext(grohl.Data{"backend": "libvirt-lxc", "fn": "CloseLogs"})
	l.logStreamMtx.Lock()
	defer l.logStreamMtx.Unlock()
	buffers := make(host.LogBuffers, len(l.logStreams))
	for id, streams := range l.logStreams {
		g.Log(grohl.Data{"job.id": id})
		buffer := make(host.LogBuffer, len(streams))
		for fd, stream := range streams {
			buffer[fd] = stream.Close()
		}
		buffers[id] = buffer
		delete(l.logStreams, id)
	}
	return buffers, nil
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

// Taken from Kubernetes:
// https://github.com/kubernetes/kubernetes/blob/d66ae29587e746c40390d61a1253a1bfa7aebd8a/pkg/kubelet/dockertools/docker.go#L323-L336
func milliCPUToShares(milliCPU int64) int64 {
	// Taken from lmctfy https://github.com/google/lmctfy/blob/master/lmctfy/controllers/cpu_controller.cc
	const (
		minShares     = 2
		sharesPerCPU  = 1024
		milliCPUToCPU = 1000
	)

	if milliCPU == 0 {
		// zero shares is invalid, 2 is the minimum
		return minShares
	}
	// Conceptually (milliCPU / milliCPUToCPU) * sharesPerCPU, but factored to improve rounding.
	shares := (milliCPU * sharesPerCPU) / milliCPUToCPU
	if shares < minShares {
		return minShares
	}
	return shares
}

func createCGroupPartition(name string, cpuShares int64) error {
	name = name + ".partition"
	for _, group := range []string{"blkio", "cpu", "cpuacct", "cpuset", "devices", "freezer", "memory", "net_cls", "perf_event"} {
		if err := os.MkdirAll(filepath.Join("/sys/fs/cgroup/", group, "machine", name), 0755); err != nil {
			return fmt.Errorf("error creating partition cgroup: %s", err)
		}
	}
	for _, param := range []string{"cpuset.cpus", "cpuset.mems"} {
		data, err := ioutil.ReadFile(filepath.Join("/sys/fs/cgroup/cpuset/machine", param))
		if err != nil {
			return fmt.Errorf("error reading cgroup param: %s", err)
		}
		if len(bytes.TrimSpace(data)) == 0 {
			// Populate our parent cgroup to avoid ENOSPC when creating containers
			data, err = ioutil.ReadFile(filepath.Join("/sys/fs/cgroup/cpuset", param))
			if err != nil {
				return fmt.Errorf("error reading cgroup param: %s", err)
			}
			if err := ioutil.WriteFile(filepath.Join("/sys/fs/cgroup/cpuset/machine", param), data, 0644); err != nil {
				return fmt.Errorf("error writing cgroup param: %s", err)
			}
		}
		if err := ioutil.WriteFile(filepath.Join("/sys/fs/cgroup/cpuset/machine", name, param), data, 0644); err != nil {
			return fmt.Errorf("error writing cgroup param: %s", err)
		}
	}
	if err := ioutil.WriteFile(filepath.Join("/sys/fs/cgroup/cpu/machine", name, "cpu.shares"), strconv.AppendInt(nil, cpuShares, 10), 0644); err != nil {
		return fmt.Errorf("error writing cgroup param: %s", err)
	}
	return nil
}
