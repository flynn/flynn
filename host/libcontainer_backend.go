package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/docker/docker/pkg/term"
	"github.com/docker/go-units"
	"github.com/docker/libcontainer/netlink"
	"github.com/docker/libnetwork/ipallocator"
	"github.com/docker/libnetwork/netutils"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/host/containerinit"
	"github.com/flynn/flynn/host/logmux"
	"github.com/flynn/flynn/host/resource"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/host/volume"
	"github.com/flynn/flynn/host/volume/manager"
	logagg "github.com/flynn/flynn/logaggregator/types"
	logutils "github.com/flynn/flynn/logaggregator/utils"
	"github.com/flynn/flynn/pkg/dialer"
	"github.com/flynn/flynn/pkg/iptables"
	"github.com/flynn/flynn/pkg/random"
	"github.com/flynn/flynn/pkg/rpcplus"
	"github.com/flynn/flynn/pkg/shutdown"
	"github.com/flynn/flynn/pkg/syslog/rfc5424"
	"github.com/flynn/flynn/pkg/verify"
	"github.com/golang/groupcache/singleflight"
	"github.com/miekg/dns"
	"github.com/opencontainers/runc/libcontainer"
	"github.com/opencontainers/runc/libcontainer/cgroups"
	"github.com/opencontainers/runc/libcontainer/configs"
	"github.com/rancher/sparse-tools/sparse"
	"gopkg.in/inconshreveable/log15.v2"
)

const (
	imageRoot         = "/var/lib/docker"
	containerRoot     = "/var/lib/flynn/container"
	defaultMountFlags = syscall.MS_NOEXEC | syscall.MS_NOSUID | syscall.MS_NODEV
	defaultPartition  = "user"
	defaultMemory     = 1 * units.GiB
	RLIMIT_NPROC      = 6
)

type LibcontainerConfig struct {
	State            *State
	VolManager       *volumemanager.Manager
	BridgeName       string
	InitPath         string
	InitLogLevel     log15.Lvl
	LogMux           *logmux.Mux
	PartitionCGroups map[string]int64
	Logger           log15.Logger
}

func NewLibcontainerBackend(config *LibcontainerConfig) (Backend, error) {
	factory, err := libcontainer.New(
		containerRoot,
		libcontainer.Cgroupfs,
		libcontainer.InitArgs(os.Args[0], "libcontainer-init"),
	)
	if err != nil {
		return nil, err
	}

	if err := setupCGroups(config.PartitionCGroups); err != nil {
		return nil, err
	}

	defaultTmpfs, err := createTmpfs(resource.DefaultTempDiskSize)
	if err != nil {
		return nil, err
	}
	shutdown.BeforeExit(func() { defaultTmpfs.Delete() })

	l := &LibcontainerBackend{
		LibcontainerConfig:  config,
		factory:             factory,
		logStreams:          make(map[string]map[string]*logmux.LogStream),
		containers:          make(map[string]*Container),
		defaultEnv:          make(map[string]string),
		resolvConf:          "/etc/resolv.conf",
		ipalloc:             ipallocator.New(),
		discoverdConfigured: make(chan struct{}),
		networkConfigured:   make(chan struct{}),
		globalState:         &libcontainerGlobalState{},
		defaultTmpfs:        defaultTmpfs,
	}
	l.httpClient = &http.Client{Transport: &http.Transport{
		Dial: dialer.RetryDial(l.discoverdDial),
	}}
	return l, nil
}

type LibcontainerBackend struct {
	*LibcontainerConfig

	factory libcontainer.Factory
	host    *Host
	ipalloc *ipallocator.IPAllocator

	bridgeAddr net.IP
	bridgeNet  *net.IPNet
	resolvConf string

	logStreamMtx sync.Mutex
	logStreams   map[string]map[string]*logmux.LogStream

	containersMtx sync.RWMutex
	containers    map[string]*Container

	envMtx     sync.RWMutex
	defaultEnv map[string]string

	discoverdConfigured chan struct{}
	networkConfigured   chan struct{}

	globalStateMtx sync.Mutex
	globalState    *libcontainerGlobalState

	defaultTmpfs    *Tmpfs
	layerLoader     singleflight.Group
	httpClient      *http.Client
	discoverdClient *discoverd.Client
}

type Container struct {
	ID        string         `json:"id"`
	RootPath  string         `json:"root_path"`
	TmpPath   string         `json:"tmp_path"`
	IP        net.IP         `json:"ip"`
	MuxConfig *logmux.Config `json:"mux_config"`

	container libcontainer.Container
	job       *host.Job
	l         *LibcontainerBackend
	done      chan struct{}

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

// ConfigureNetworking is called once during host startup and sets up the local
// bridge and forwarding rules for containers.
func (l *LibcontainerBackend) ConfigureNetworking(config *host.NetworkConfig) error {
	log := l.Logger.New("fn", "ConfigureNetworking")
	var err error
	l.bridgeAddr, l.bridgeNet, err = net.ParseCIDR(config.Subnet)
	if err != nil {
		return err
	}
	l.ipalloc.RequestIP(l.bridgeNet, l.bridgeAddr)

	err = netlink.CreateBridge(l.BridgeName, false)
	bridgeExists := os.IsExist(err)
	if err != nil && !bridgeExists {
		return err
	}

	bridge, err := net.InterfaceByName(l.BridgeName)
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

	// enable IP forwarding
	ipFwd := "/proc/sys/net/ipv4/ip_forward"
	if data, err := ioutil.ReadFile(ipFwd); err != nil && !bytes.HasPrefix(data, []byte("1")) {
		if err := ioutil.WriteFile(ipFwd, []byte("1\n"), 0644); err != nil {
			return err
		}
	}

	// Set up iptables for outbound traffic masquerading from containers to the
	// rest of the network.
	if err := iptables.EnableOutboundNAT(l.BridgeName, l.bridgeNet.String()); err != nil {
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
	l.containersMtx.Lock()
	defer l.containersMtx.Unlock()
	for _, container := range l.containers {
		if !container.job.Config.HostNetwork {
			if _, err := l.ipalloc.RequestIP(l.bridgeNet, container.IP); err != nil {
				log.Error("error requesting ip", "job.id", container.job.ID, "err", err)
			}
		}
	}

	close(l.networkConfigured)

	return nil
}

func (l *LibcontainerBackend) SetDefaultEnv(k, v string) {
	l.envMtx.Lock()
	l.defaultEnv[k] = v
	l.envMtx.Unlock()
	if k == "DISCOVERD" {
		l.discoverdClient = discoverd.NewClientWithURL(v)
		close(l.discoverdConfigured)
	}
}

func (l *LibcontainerBackend) SetHost(h *Host) {
	l.host = h
}

func (l *LibcontainerBackend) Run(job *host.Job, runConfig *RunConfig, rateLimitBucket *RateLimitBucket) (err error) {
	log := l.Logger.New("fn", "run", "job.id", job.ID)

	// if the job has been stopped, just return
	if l.State.GetJob(job.ID).ForceStop {
		log.Info("skipping start of stopped job")
		return nil
	}

	log.Info("starting job", "job.args", job.Config.Args)

	defer func() {
		if err != nil {
			l.State.SetStatusFailed(job.ID, err)
		}
	}()

	if job.Partition == "" {
		job.Partition = defaultPartition
	}
	if _, ok := l.PartitionCGroups[job.Partition]; !ok {
		return fmt.Errorf("host: invalid job partition %q", job.Partition)
	}

	wait := func(ch chan struct{}) {
		if rateLimitBucket != nil {
			// unblock the rate limiter whilst waiting
			rateLimitBucket.Put()
			defer rateLimitBucket.Wait()
		}
		<-ch
	}
	if !job.Config.HostNetwork {
		wait(l.networkConfigured)
	}
	if _, ok := job.Config.Env["DISCOVERD"]; !ok {
		wait(l.discoverdConfigured)
	}

	if runConfig == nil {
		runConfig = &RunConfig{}
	}
	container := &Container{
		ID: job.ID,
		MuxConfig: &logmux.Config{
			AppID:   job.Metadata["flynn-controller.app"],
			HostID:  l.State.id,
			JobType: job.Metadata["flynn-controller.type"],
			JobID:   job.ID,
		},
		l:    l,
		job:  job,
		done: make(chan struct{}),
	}
	if !job.Config.HostNetwork {
		container.IP, err = l.ipalloc.RequestIP(l.bridgeNet, runConfig.IP)
		if err != nil {
			log.Error("error requesting ip", "err", err)
			return err
		}
		log.Info("obtained ip", "network", l.bridgeNet.String(), "ip", container.IP.String())
		l.State.SetContainerIP(job.ID, container.IP)
	}
	defer func() {
		if err != nil {
			go container.cleanup()
		}
	}()

	log.Info("setting up rootfs")
	rootPath := filepath.Join("/var/lib/flynn/image/mnt", job.ID)
	tmpPath := filepath.Join("/var/lib/flynn/image/tmp", job.ID)
	for _, path := range []string{rootPath, tmpPath} {
		if err := os.MkdirAll(path, 0755); err != nil {
			log.Error("error setting up rootfs", "err", err)
			return err
		}
	}
	rootMount, err := l.rootOverlayMount(job)
	if err != nil {
		log.Error("error setting up rootfs", "err", err)
		return err
	}

	container.RootPath = rootPath
	container.TmpPath = tmpPath

	cgroupMountFlags := defaultMountFlags
	if !job.Config.WriteableCgroups {
		cgroupMountFlags |= syscall.MS_RDONLY
	}

	if job.Config.LinuxCapabilities == nil {
		job.Config.LinuxCapabilities = &host.DefaultCapabilities
	}
	if job.Config.AllowedDevices == nil {
		job.Config.AllowedDevices = &host.DefaultAllowedDevices
	}
	config := &configs.Config{
		Rootfs:       rootPath,
		Capabilities: *job.Config.LinuxCapabilities,
		Namespaces: configs.Namespaces([]configs.Namespace{
			{Type: configs.NEWNS},
			{Type: configs.NEWUTS},
			{Type: configs.NEWIPC},
			{Type: configs.NEWPID},
		}),
		Cgroups: &configs.Cgroup{
			Path: filepath.Join("/flynn", job.Partition, job.ID),
			Resources: &configs.Resources{
				AllowedDevices: *job.Config.AllowedDevices,
				Memory:         defaultMemory,
			},
		},
		MaskPaths: []string{
			"/proc/kcore",
		},
		ReadonlyPaths: []string{
			"/proc/sys", "/proc/sysrq-trigger", "/proc/irq", "/proc/bus",
		},
		Devices: configs.DefaultAutoCreatedDevices,
		Mounts: append([]*configs.Mount{rootMount}, []*configs.Mount{
			{
				Source:      "proc",
				Destination: "/proc",
				Device:      "proc",
				Flags:       defaultMountFlags,
			},
			{
				Source:      "sysfs",
				Destination: "/sys",
				Device:      "sysfs",
				Flags:       defaultMountFlags | syscall.MS_RDONLY,
			},
			{
				Source:      "tmpfs",
				Destination: "/dev",
				Device:      "tmpfs",
				Flags:       syscall.MS_NOSUID | syscall.MS_STRICTATIME,
				Data:        "mode=755",
			},
			{
				Source:      "devpts",
				Destination: "/dev/pts",
				Device:      "devpts",
				Flags:       syscall.MS_NOSUID | syscall.MS_NOEXEC,
				Data:        "newinstance,ptmxmode=0666,mode=0620,gid=5",
			},
			{
				Device:      "tmpfs",
				Source:      "shm",
				Destination: "/dev/shm",
				Data:        "mode=1777,size=65536k",
				Flags:       defaultMountFlags,
			},
			{
				Destination: "/sys/fs/cgroup",
				Device:      "cgroup",
				Flags:       cgroupMountFlags,
			},
		}...),
	}

	if spec, ok := job.Resources[resource.TypeMaxFD]; ok && spec.Limit != nil && spec.Request != nil {
		log.Info(fmt.Sprintf("setting max fd limit to %d / %d", *spec.Request, *spec.Limit))
		config.Rlimits = append(config.Rlimits, configs.Rlimit{
			Type: syscall.RLIMIT_NOFILE,
			Hard: uint64(*spec.Limit),
			Soft: uint64(*spec.Request),
		})
	}

	if spec, ok := job.Resources[resource.TypeMaxProcs]; ok && spec.Limit != nil && spec.Request != nil {
		log.Info(fmt.Sprintf("setting max processes limit to %d / %d", *spec.Request, *spec.Limit))
		config.Rlimits = append(config.Rlimits, configs.Rlimit{
			Type: RLIMIT_NPROC,
			Hard: uint64(*spec.Limit),
			Soft: uint64(*spec.Request),
		})
	}

	log.Info("mounting container directories and files")
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
	if err := os.MkdirAll(filepath.Join(tmpPath, "etc"), 0755); err != nil {
		log.Error("error creating container /etc", "err", err)
		return err
	}
	etcHosts := filepath.Join(tmpPath, "etc/hosts")
	if err := writeHostname(etcHosts, hostname); err != nil {
		log.Error("error writing hosts file", "err", err)
		return err
	}
	sharedDir := filepath.Join(tmpPath, ".container-shared")
	if err := os.MkdirAll(sharedDir, 0700); err != nil {
		log.Error("error creating .container-shared", "err", err)
		return err
	}

	config.Mounts = append(config.Mounts,
		bindMount(l.InitPath, "/.containerinit", false),
		bindMount(l.resolvConf, "/etc/resolv.conf", false),
		bindMount(etcHosts, "/etc/hosts", true),
		bindMount(sharedDir, "/.container-shared", true),
	)
	for _, m := range job.Config.Mounts {
		if m.Target == "" {
			return errors.New("host: invalid empty mount target")
		}
		config.Mounts = append(config.Mounts, bindMount(m.Target, m.Location, m.Writeable))
	}

	// apply volumes
	for _, v := range job.Config.Volumes {
		vol := l.VolManager.GetVolume(v.VolumeID)
		if vol == nil {
			err := fmt.Errorf("job %s required volume %s, but that volume does not exist", job.ID, v.VolumeID)
			log.Error("missing required volume", "volumeID", v.VolumeID, "err", err)
			return err
		}
		config.Mounts = append(config.Mounts, bindMount(vol.Location(), v.Target, v.Writeable))
	}

	// mutating job state, take state write lock
	l.State.mtx.Lock()
	if job.Config.Env == nil {
		job.Config.Env = make(map[string]string)
	}
	for i, p := range job.Config.Ports {
		if p.Proto != "tcp" && p.Proto != "udp" {
			err := fmt.Errorf("unknown port proto %q", p.Proto)
			log.Error("error allocating port", "proto", p.Proto, "err", err)
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
	// release the write lock, we won't mutate global structures from here on out
	l.State.mtx.Unlock()

	initConfig := &containerinit.Config{
		Args:      job.Config.Args,
		TTY:       job.Config.TTY,
		OpenStdin: job.Config.Stdin,
		WorkDir:   job.Config.WorkingDir,
		Uid:       job.Config.Uid,
		Gid:       job.Config.Gid,
		Resources: job.Resources,
		LogLevel:  l.InitLogLevel,
	}
	if !job.Config.HostNetwork {
		initConfig.IP = container.IP.String() + "/24"
		initConfig.Gateway = l.bridgeAddr.String()
	}
	for _, port := range job.Config.Ports {
		initConfig.Ports = append(initConfig.Ports, port)
	}

	log.Info("writing config")
	configPath := filepath.Join(tmpPath, ".containerconfig")
	l.envMtx.RLock()
	err = writeContainerConfig(configPath, initConfig,
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
		log.Error("error writing config", "err", err)
		return err
	}
	config.Mounts = append(config.Mounts, bindMount(configPath, "/.containerconfig", false))

	if job.Config.HostNetwork {
		// allow host network jobs to configure the network
		config.Capabilities = append(config.Capabilities, "CAP_NET_ADMIN")
	} else {
		ifaceName, err := netutils.GenerateIfaceName("veth", 4)
		if err != nil {
			return err
		}
		config.Hostname = hostname
		config.Namespaces = append(config.Namespaces, configs.Namespace{Type: configs.NEWNET})
		config.Networks = []*configs.Network{
			{
				Type:    "loopback",
				Address: "127.0.0.1/0",
				Gateway: "localhost",
			},
			{
				Type:              "veth",
				Name:              "eth0",
				Bridge:            l.BridgeName,
				Address:           initConfig.IP,
				Gateway:           initConfig.Gateway,
				Mtu:               1500,
				HostInterfaceName: ifaceName,
			},
		}
	}
	if spec, ok := job.Resources[resource.TypeMemory]; ok && spec.Limit != nil {
		config.Cgroups.Resources.Memory = *spec.Limit
	}
	if spec, ok := job.Resources[resource.TypeCPU]; ok && spec.Limit != nil {
		config.Cgroups.Resources.CpuShares = milliCPUToShares(*spec.Limit)
	}

	c, err := l.factory.Create(job.ID, config)
	if err != nil {
		return err
	}

	process := &libcontainer.Process{
		Args: []string{"/.containerinit", job.ID},
		User: "root",
	}
	if err := c.Run(process); err != nil {
		c.Destroy()
		return err
	}
	go process.Wait()

	container.container = c

	go container.watch(nil, nil)

	log.Info("job started")
	return nil
}

func (l *LibcontainerBackend) rootOverlayMount(job *host.Job) (*configs.Mount, error) {
	log := l.Logger.New("fn", "rootOverlayMount", "job.id", job.ID)
	layers := make([]string, 0, len(job.Mountspecs)+1)
	for _, spec := range job.Mountspecs {
		if spec.Type != host.MountspecTypeSquashfs {
			return nil, fmt.Errorf("unknown mountspec type: %q", spec.Type)
		}
		log.Info("mounting squashfs layer", "id", spec.ID)
		path, err := l.mountSquashfs(spec)
		if err != nil {
			return nil, err
		}
		layers = append(layers, path)
	}
	log.Info("mounting ext2 layer")
	tmpfs, err := l.mountTmpfs(job)
	if err != nil {
		return nil, err
	}
	layers = append(layers, tmpfs)
	dirs := make([]string, len(layers))
	for i, layer := range layers {
		// append mount paths in reverse order as overlay
		// lower dirs are stacked from right to left
		dirs[len(layers)-i-1] = layer
	}
	upperDir := filepath.Join(tmpfs, "overlay-upperdir")
	workDir := filepath.Join(tmpfs, "overlay-workdir")
	for _, dir := range []string{upperDir, workDir} {
		if err := os.Mkdir(dir, 0755); err != nil {
			return nil, err
		}
	}
	return &configs.Mount{
		Source:      "overlay",
		Destination: "/",
		Device:      "overlay",
		Data:        fmt.Sprintf("lowerdir=%s,upperdir=%s,workdir=%s", strings.Join(dirs[1:], ":"), upperDir, workDir),
	}, nil
}

func (l *LibcontainerBackend) mountSquashfs(m *host.Mountspec) (string, error) {
	// use the layerLoader to ensure only one caller downloads any
	// given layer ID
	path, err := l.layerLoader.Do(m.ID, func() (interface{}, error) {
		if vol := l.VolManager.GetVolume(m.ID); vol != nil {
			return vol.Location(), nil
		}

		if m.URL == "" {
			return "", fmt.Errorf("error getting squashfs layer %s: missing URL", m.ID)
		}

		verifier, err := verify.NewVerifier(m.Hashes, m.Size)
		if err != nil {
			return "", fmt.Errorf("error getting squashfs layer %s: %s", m.ID, err)
		}

		u, err := url.Parse(m.URL)
		if err != nil {
			return "", err
		}

		var layer io.ReadCloser
		switch u.Scheme {
		case "file":
			f, err := os.Open(u.Path)
			if err != nil {
				return "", fmt.Errorf("error getting squashfs layer %s: %s", m.URL, err)
			}
			layer = f
		case "http", "https":
			res, err := l.httpClient.Get(m.URL)
			if err != nil {
				return "", fmt.Errorf("error getting squashfs layer from %s: %s", m.URL, err)
			}
			if res.StatusCode != http.StatusOK {
				return "", fmt.Errorf("error getting squashfs layer from %s: unexpected HTTP status %s", m.URL, res.Status)
			}
			layer = res.Body
		default:
			return "", fmt.Errorf("unknown layer URI scheme: %s", u.Scheme)
		}
		defer layer.Close()

		// write the layer to a temp file and verify it has the
		// expected hashes
		tmp, err := ioutil.TempFile("", "flynn-layer-")
		if err != nil {
			return "", err
		}
		defer os.Remove(tmp.Name())
		defer tmp.Close()
		if _, err := io.Copy(tmp, verifier.Reader(layer)); err != nil {
			return "", fmt.Errorf("error getting squashfs layer from %s: %s", m.URL, err)
		}
		if err := verifier.Verify(); err != nil {
			return "", fmt.Errorf("error getting squashfs layer from %s: %s", m.URL, err)
		}

		if _, err := tmp.Seek(0, os.SEEK_SET); err != nil {
			return "", fmt.Errorf("error seeking squashfs layer temp file: %s", err)
		}
		vol, err := l.VolManager.ImportFilesystem("default", &volume.Filesystem{
			ID:         m.ID,
			Data:       tmp,
			Size:       m.Size,
			Type:       volume.VolumeTypeSquashfs,
			MountFlags: syscall.MS_RDONLY,
			Meta:       m.Meta,
		})
		if err != nil {
			return "", fmt.Errorf("error importing squashfs layer: %s", err)
		}

		return vol.Location(), nil
	})
	if err != nil {
		return "", err
	}
	return path.(string), nil
}

func (l *LibcontainerBackend) mountTmpfs(job *host.Job) (string, error) {
	tmpfs := l.defaultTmpfs
	if spec, ok := job.Resources[resource.TypeTempDisk]; ok && spec.Limit != nil && *spec.Limit != tmpfs.Size {
		var err error
		tmpfs, err = createTmpfs(*spec.Limit)
		if err != nil {
			return "", err
		}
		defer tmpfs.Delete()
	}

	f, err := os.Open(tmpfs.Path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	vol, err := l.VolManager.ImportFilesystem("default", &volume.Filesystem{
		ID:         job.ID,
		Data:       sparse.NewBufferedFileIoProcessorByFP(f),
		Size:       tmpfs.Size,
		Type:       volume.VolumeTypeExt2,
		MountFlags: syscall.MS_NOATIME,
	})
	if err != nil {
		return "", fmt.Errorf("error importing tmpfs: %s", err)
	}

	return vol.Location(), nil
}

// discoverdDial is a discoverd aware dialer which resolves a discoverd host to
// an address using the configured discoverd client as the host is likely not
// using discoverd to resolve DNS queries
func (l *LibcontainerBackend) discoverdDial(network, addr string) (net.Conn, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	if strings.HasSuffix(host, ".discoverd") {
		// ensure discoverd is configured
		<-l.discoverdConfigured
		// lookup the service and pick a random address
		service := strings.TrimSuffix(host, ".discoverd")
		addrs, err := l.discoverdClient.Service(service).Addrs()
		if err != nil {
			return nil, err
		} else if len(addrs) == 0 {
			return nil, fmt.Errorf("lookup %s: no such host", host)
		}
		addr = addrs[random.Math.Intn(len(addrs))]
	}
	return dialer.Default.Dial(network, addr)
}

func (c *Container) watch(ready chan<- error, buffer host.LogBuffer) error {
	log := c.l.Logger.New("fn", "watch", "job.id", c.job.ID)
	log.Info("start watching container")

	readyErr := func(err error) {
		if ready != nil {
			ready <- err
		}
	}

	defer func() {
		c.container.Destroy()
		c.l.containersMtx.Lock()
		delete(c.l.containers, c.job.ID)
		c.l.containersMtx.Unlock()
		c.cleanup()
		close(c.done)
	}()

	var symlinked bool
	var err error
	symlink := "/tmp/containerinit-rpc." + c.job.ID
	socketPath := path.Join(c.TmpPath, containerinit.SocketPath)
	for startTime := time.Now(); time.Since(startTime) < 10*time.Second; time.Sleep(time.Millisecond) {
		if !symlinked {
			// We can't connect to the socket file directly because
			// the path to it is longer than 108 characters (UNIX_PATH_MAX).
			// Create a temporary symlink to connect to.
			if err = os.Symlink(socketPath, symlink); err != nil && !os.IsExist(err) {
				log.Error("error symlinking socket", "err", err)
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
	if err != nil {
		log.Error("error connecting to container", "err", err)
		readyErr(err)
		c.l.State.SetStatusFailed(c.job.ID, errors.New("failed to connect to container"))
		return err
	}
	defer c.Client.Close()

	c.l.containersMtx.Lock()
	c.l.containers[c.job.ID] = c
	c.l.containersMtx.Unlock()

	readyErr(nil)

	if !c.job.Config.DisableLog && !c.job.Config.TTY {
		if err := c.followLogs(log, buffer); err != nil {
			return err
		}
	}

	notifyOOM, err := c.container.NotifyOOM()
	if err != nil {
		log.Error("error subscribing to OOM notifications", "err", err)
		return err
	}
	go func() {
		logger := c.l.LogMux.Logger(logagg.MsgIDInit, c.MuxConfig, "component", "flynn-host")
		defer logger.Close()
		for range notifyOOM {
			logger.Crit("FATAL: a container process was killed due to lack of available memory")
		}
	}()

	log.Info("watching for changes")
	for change := range c.Client.StreamState() {
		log.Info("state change", "state", change.State.String())
		if change.Error != "" {
			err := errors.New(change.Error)
			log.Error("error in change state", "err", err)
			c.Client.Resume()
			c.l.State.SetStatusFailed(c.job.ID, err)
			return err
		}
		switch change.State {
		case containerinit.StateInitial:
			log.Info("waiting for attach")
			c.l.State.WaitAttach(c.job.ID)
			log.Info("resuming")
			c.Client.Resume()
			log.Info("resumed")
		case containerinit.StateRunning:
			log.Info("container running")
			c.l.State.SetStatusRunning(c.job.ID)

			// if the job was stopped before it started, exit
			if c.l.State.GetJob(c.job.ID).ForceStop {
				c.Stop()
			}
		case containerinit.StateExited:
			log.Info("container exited", "status", change.ExitStatus)
			c.Client.Resume()
			c.l.State.SetStatusDone(c.job.ID, change.ExitStatus)
			return nil
		case containerinit.StateFailed:
			log.Info("container failed to start")
			c.Client.Resume()
			c.l.State.SetStatusFailed(c.job.ID, errors.New("container failed to start"))
			return nil
		}
	}
	log.Error("unknown failure")
	c.l.State.SetStatusFailed(c.job.ID, errors.New("unknown failure"))

	return nil
}

func (c *Container) followLogs(log log15.Logger, buffer host.LogBuffer) error {
	c.l.logStreamMtx.Lock()
	defer c.l.logStreamMtx.Unlock()
	if _, ok := c.l.logStreams[c.job.ID]; ok {
		return nil
	}

	log.Info("getting stdout")
	stdout, stderr, initLog, err := c.Client.GetStreams()
	if err != nil {
		log.Error("error getting streams", "err", err)
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

	logStreams := make(map[string]*logmux.LogStream, 3)
	stdoutR, err := nonblocking(stdout)
	if err != nil {
		log.Error("error streaming stdout", "err", err)
		return err
	}
	logStreams["stdout"] = c.l.LogMux.Follow(stdoutR, buffer["stdout"], logagg.MsgIDStdout, c.MuxConfig)

	stderrR, err := nonblocking(stderr)
	if err != nil {
		log.Error("error streaming stderr", "err", err)
		return err
	}
	logStreams["stderr"] = c.l.LogMux.Follow(stderrR, buffer["stderr"], logagg.MsgIDStderr, c.MuxConfig)

	initLogR, err := nonblocking(initLog)
	if err != nil {
		log.Error("error streaming initial log", "err", err)
		return err
	}
	logStreams["initLog"] = c.l.LogMux.Follow(initLogR, buffer["initLog"], logagg.MsgIDInit, c.MuxConfig)
	c.l.logStreams[c.job.ID] = logStreams

	return nil
}

func (c *Container) cleanup() error {
	log := c.l.Logger.New("fn", "cleanup", "job.id", c.job.ID)
	log.Info("starting cleanup")

	c.l.logStreamMtx.Lock()
	for _, s := range c.l.logStreams[c.job.ID] {
		s.Close()
	}
	delete(c.l.logStreams, c.job.ID)
	c.l.logStreamMtx.Unlock()

	if !c.job.Config.HostNetwork && c.l.bridgeNet != nil {
		c.l.ipalloc.ReleaseIP(c.l.bridgeNet, c.IP)
	}
	for _, v := range c.job.Config.Volumes {
		if !v.DeleteOnStop {
			continue
		}
		if err := c.l.VolManager.DestroyVolume(v.VolumeID); err != nil {
			log.Error("error destroying volume", "vol.id", v.VolumeID, "err", err)
		}
	}

	// remove the tmpfs volume (which has the same ID as the job)
	if err := c.l.VolManager.DestroyVolume(c.job.ID); err != nil {
		log.Error("error removing tmpfs volume", "err", err)
	}

	os.RemoveAll(c.TmpPath)
	log.Info("finished cleanup")
	c.l.State.SendCleanupEvent(c.job.ID)
	return nil
}

func (c *Container) WaitStop(timeout time.Duration) error {
	job := c.l.State.GetJob(c.job.ID)
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

func (c *Container) Stop() error {
	if err := c.Signal(int(syscall.SIGTERM)); err != nil {
		return err
	}
	if err := c.WaitStop(10 * time.Second); err != nil {
		return c.Signal(int(syscall.SIGKILL))
	}
	return nil
}

func (l *LibcontainerBackend) Stop(id string) error {
	c, err := l.getContainer(id)
	if err != nil {
		return err
	}
	err = c.Stop()
	if err == rpcplus.ErrShutdown {
		// if the process is disconnected, the stop was probably successful
		err = nil
	}
	return err
}

func (l *LibcontainerBackend) JobExists(id string) bool {
	l.containersMtx.RLock()
	defer l.containersMtx.RUnlock()
	_, ok := l.containers[id]
	return ok
}

func (l *LibcontainerBackend) getContainer(id string) (*Container, error) {
	l.containersMtx.RLock()
	defer l.containersMtx.RUnlock()
	c := l.containers[id]
	if c == nil {
		return nil, errors.New("unknown container")
	}
	return c, nil
}

func (l *LibcontainerBackend) ResizeTTY(id string, height, width uint16) error {
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
	defer pty.Close()
	return term.SetWinsize(pty.Fd(), &term.Winsize{Height: height, Width: width})
}

func (l *LibcontainerBackend) Signal(id string, sig int) error {
	container, err := l.getContainer(id)
	if err != nil {
		return err
	}
	return container.Signal(sig)
}

func (l *LibcontainerBackend) DiscoverdDeregister(id string) error {
	container, err := l.getContainer(id)
	if err != nil {
		return err
	}
	return container.DiscoverdDeregister()
}

func (l *LibcontainerBackend) Attach(req *AttachRequest) (err error) {
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
			job := l.State.GetJob(req.Job.Job.ID)
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
		defer pty.Close()
		if err := term.SetWinsize(pty.Fd(), &term.Winsize{Height: req.Height, Width: req.Width}); err != nil {
			return err
		}
		if req.Attached != nil {
			req.Attached <- struct{}{}
		}

		done := make(chan struct{}, 2)
		if req.Stdin != nil {
			go func() {
				io.Copy(pty, req.Stdin)
				done <- struct{}{}
			}()
		}
		if req.Stdout != nil {
			go func() {
				io.Copy(req.Stdout, pty)
				done <- struct{}{}
			}()
		}

		<-done
		l.Logger.Info("one side of the TTY went away, stopping job", "fn", "attach", "job.id", req.Job.Job.ID)
		client.Stop()
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
		defer stdout.Close()
		defer stderr.Close()
		defer initLog.Close()
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
	stream, err := l.LogMux.StreamLog(req.Job.Job.Metadata["flynn-controller.app"], req.Job.Job.ID, req.Logs, req.Stream, ch)
	if err != nil {
		return err
	}
	defer stream.Close()

	for msg := range ch {
		var w io.Writer
		switch logutils.StreamType(msg) {
		case logagg.StreamTypeStdout:
			w = req.Stdout
		case logagg.StreamTypeStderr:
			w = req.Stderr
		case logagg.StreamTypeInit:
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

func (l *LibcontainerBackend) Cleanup(except []string) error {
	log := l.Logger.New("fn", "Cleanup")
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
	log.Info("starting cleanup", "count", len(ids))
	errs := make(chan error)
	for _, id := range ids {
		go func(id string) {
			log.Info("stopping job", "job.id", id)
			err := l.Stop(id)
			if err != nil {
				log.Error("error stopping job", "job.id", id, "err", err)
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
	log.Info("finished")
	return err
}

type libcontainerGlobalState struct {
	NetworkConfig   *host.NetworkConfig
	DiscoverdConfig *host.DiscoverdConfig
}

func (l *LibcontainerBackend) persistGlobalState() error {
	data, _ := json.Marshal(l.globalState)
	return l.State.PersistBackendGlobalState(data)
}

/*
	Loads a series of jobs, and reconstructs whatever additional backend state was saved.

	This may include reconnecting rpc systems and communicating with containers
	(thus this may take a significant moment; it's not just deserializing).
*/
func (l *LibcontainerBackend) UnmarshalState(jobs map[string]*host.ActiveJob, jobBackendStates map[string][]byte, backendGlobalState []byte, buffers host.LogBuffers) error {
	log := l.Logger.New("fn", "UnmarshalState")
	containers := make(map[string]*Container)
	for k, v := range jobBackendStates {
		container := &Container{}
		if err := json.Unmarshal(v, container); err != nil {
			return fmt.Errorf("failed to deserialize backed container state: %s", err)
		}
		if container.MuxConfig == nil {
			container.MuxConfig = &logmux.Config{}
		}
		container.MuxConfig.HostID = l.State.id
		c, err := l.factory.Load(container.ID)
		if err != nil {
			return fmt.Errorf("error loading container state: %s", err)
		}
		container.container = c
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
		container.MuxConfig.AppID = j.Job.Metadata["flynn-controller.app"]
		container.MuxConfig.JobType = j.Job.Metadata["flynn-controller.type"]
		container.MuxConfig.JobID = j.Job.ID
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
			delete(readySignals, j.Job.ID)
			continue
		}
		log.Info("reconnected to running container", "job.id", j.Job.ID)
	}
	if len(backendGlobalState) > 0 {
		state := &libcontainerGlobalState{}
		if err := json.Unmarshal(backendGlobalState, state); err != nil {
			return err
		}
		log.Info("using stored global backend config")

		if state.NetworkConfig != nil && state.NetworkConfig.JobID != "" {
			if _, ok := readySignals[state.NetworkConfig.JobID]; ok {
				log.Info("using stored network config", "job.id", state.NetworkConfig.JobID)

				// run ConfigureNetworking in a goroutine to avoid deadlock
				// between state.Restore and PersistGlobalState which both
				// access the state database
				go l.host.ConfigureNetworking(state.NetworkConfig)
			} else {
				log.Info("got stored network config, but associated job isn't running", "job.id", state.NetworkConfig.JobID)
			}
		}

		if state.DiscoverdConfig != nil && state.DiscoverdConfig.JobID != "" {
			if _, ok := readySignals[state.DiscoverdConfig.JobID]; ok {
				log.Info("using stored discoverd config", "job.id", state.DiscoverdConfig.JobID)

				// run ConfigureDiscoverd in a goroutine to avoid deadlock
				// between state.Restore and PersistGlobalState which both
				// access the state database
				go l.host.ConfigureDiscoverd(state.DiscoverdConfig)
			} else {
				log.Info("got stored discoverd config, but associated job isn't running", "job.id", state.DiscoverdConfig.JobID)
			}
		}
	} else {
		log.Info("no stored global backend config")

	}
	return nil
}

func (l *LibcontainerBackend) SetDiscoverdConfig(config *host.DiscoverdConfig) {
	l.globalStateMtx.Lock()
	l.globalState.DiscoverdConfig = config
	l.persistGlobalState()
	l.globalStateMtx.Unlock()
}

func (l *LibcontainerBackend) SetNetworkConfig(config *host.NetworkConfig) {
	l.globalStateMtx.Lock()
	l.globalState.NetworkConfig = config
	l.persistGlobalState()
	l.globalStateMtx.Unlock()
}

func (l *LibcontainerBackend) MarshalJobState(jobID string) ([]byte, error) {
	l.containersMtx.RLock()
	defer l.containersMtx.RUnlock()
	if associatedState, exists := l.containers[jobID]; exists {
		return json.Marshal(associatedState)
	}
	return nil, nil
}

func (l *LibcontainerBackend) OpenLogs(buffers host.LogBuffers) error {
	l.containersMtx.RLock()
	defer l.containersMtx.RUnlock()
	for id, c := range l.containers {
		if err := c.followLogs(l.Logger.New("fn", "OpenLogs", "job.id", id), buffers[id]); err != nil {
			return err
		}
	}
	return nil
}

func (l *LibcontainerBackend) CloseLogs() (host.LogBuffers, error) {
	log := l.Logger.New("fn", "CloseLogs")
	l.logStreamMtx.Lock()
	defer l.logStreamMtx.Unlock()
	buffers := make(host.LogBuffers, len(l.logStreams))
	for id, streams := range l.logStreams {
		log.Info("closing", "job.id", id)
		buffer := make(host.LogBuffer, len(streams))
		for fd, stream := range streams {
			buffer[fd] = stream.Close()
		}
		buffers[id] = buffer
		delete(l.logStreams, id)
	}
	return buffers, nil
}

func bindMount(src, dest string, writeable bool) *configs.Mount {
	flags := syscall.MS_BIND | syscall.MS_REC
	if !writeable {
		flags |= syscall.MS_RDONLY
	}
	return &configs.Mount{
		Source:      src,
		Destination: dest,
		Device:      "bind",
		Flags:       flags,
	}
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

const cgroupRoot = "/sys/fs/cgroup"

func setupCGroups(partitions map[string]int64) error {
	subsystems, err := cgroups.GetAllSubsystems()
	if err != nil {
		return fmt.Errorf("error getting cgroup subsystems: %s", err)
	} else if len(subsystems) == 0 {
		return fmt.Errorf("failed to detect any cgroup subsystems")
	}

	for _, subsystem := range subsystems {
		if _, err := cgroups.FindCgroupMountpoint(subsystem); err == nil {
			// subsystem already mounted
			continue
		}
		path := filepath.Join(cgroupRoot, subsystem)
		if err := os.Mkdir(path, 0755); err != nil && !os.IsExist(err) {
			return fmt.Errorf("error creating %s cgroup directory: %s", subsystem, err)
		}
		if err := syscall.Mount("cgroup", path, "cgroup", 0, subsystem); err != nil {
			return fmt.Errorf("error mounting %s cgroup: %s", subsystem, err)
		}
	}

	for name, shares := range partitions {
		if err := createCGroupPartition(name, shares); err != nil {
			return err
		}
	}
	return nil
}

func createCGroupPartition(name string, cpuShares int64) error {
	for _, group := range []string{"blkio", "cpu", "cpuacct", "cpuset", "devices", "freezer", "memory", "net_cls", "perf_event"} {
		if err := os.MkdirAll(filepath.Join(cgroupRoot, group, "flynn", name), 0755); err != nil {
			return fmt.Errorf("error creating partition cgroup: %s", err)
		}
	}
	for _, param := range []string{"cpuset.cpus", "cpuset.mems"} {
		data, err := ioutil.ReadFile(filepath.Join(cgroupRoot, "cpuset", "flynn", param))
		if err != nil {
			return fmt.Errorf("error reading cgroup param: %s", err)
		}
		if len(bytes.TrimSpace(data)) == 0 {
			// Populate our parent cgroup to avoid ENOSPC when creating containers
			data, err = ioutil.ReadFile(filepath.Join(cgroupRoot, "cpuset", param))
			if err != nil {
				return fmt.Errorf("error reading cgroup param: %s", err)
			}
			if err := ioutil.WriteFile(filepath.Join(cgroupRoot, "cpuset", "flynn", param), data, 0644); err != nil {
				return fmt.Errorf("error writing cgroup param: %s", err)
			}
		}
		if err := ioutil.WriteFile(filepath.Join(cgroupRoot, "cpuset", "flynn", name, param), data, 0644); err != nil {
			return fmt.Errorf("error writing cgroup param: %s", err)
		}
	}
	if err := ioutil.WriteFile(filepath.Join(cgroupRoot, "cpu", "flynn", name, "cpu.shares"), strconv.AppendInt(nil, cpuShares, 10), 0644); err != nil {
		return fmt.Errorf("error writing cgroup param: %s", err)
	}
	return nil
}

type Tmpfs struct {
	Path string
	Size int64
}

func (t *Tmpfs) Delete() error {
	return os.Remove(t.Path)
}

func createTmpfs(size int64) (*Tmpfs, error) {
	f, err := ioutil.TempFile("", "flynn-ext2-")
	if err != nil {
		return nil, err
	}
	defer f.Close()

	if err := f.Truncate(size); err != nil {
		return nil, err
	}

	cmd := exec.Command("mkfs.ext2", "-F", "-L", "rootfs", "-m", "0", f.Name())
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("error creating ext2 filesystem: %s: %s", err, out)
	}

	return &Tmpfs{Path: f.Name(), Size: size}, nil
}
