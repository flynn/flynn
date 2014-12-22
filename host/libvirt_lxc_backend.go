package main

import (
	"bufio"
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
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/natefinch/lumberjack"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/technoweenie/grohl"
	"github.com/flynn/flynn/host/containerinit"
	lt "github.com/flynn/flynn/host/libvirt"
	"github.com/flynn/flynn/host/logbuf"
	"github.com/flynn/flynn/host/ports"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/pinkerton"
	"github.com/flynn/flynn/pkg/cluster"
	"github.com/flynn/flynn/pkg/iptables"
	"github.com/flynn/flynn/pkg/random"
)

const (
	libvirtNetName = "flynn"
	bridgeName     = "flynnbr0"
	bridgeMask     = "255.255.255.0"
)

// TODO: read these from a configurable libvirt network
var bridgeAddr, bridgeNet, _ = net.ParseCIDR("192.168.200.1/24")

func NewLibvirtLXCBackend(state *State, portAlloc map[string]*ports.Allocator, volPath, logPath, initPath string) (Backend, error) {
	libvirtc, err := libvirt.NewVirConnection("lxc:///")
	if err != nil {
		return nil, err
	}

	pinkertonCtx, err := pinkerton.BuildContext("aufs", "/var/lib/docker")
	if err != nil {
		return nil, err
	}

	if err := writeResolvConf("/etc/flynn/resolv.conf"); err != nil {
		return nil, fmt.Errorf("Could not create resolv.conf: %s", err)
	}

	b := random.Bytes(5)
	bridgeMAC := fmt.Sprintf("fe:%02x:%02x:%02x:%02x:%02x", b[0], b[1], b[2], b[3], b[4])

	network, err := libvirtc.LookupNetworkByName(libvirtNetName)
	if err != nil {
		n := &lt.Network{
			Name:   libvirtNetName,
			Bridge: lt.Bridge{Name: bridgeName, STP: "off"},
			IP:     lt.IP{Address: bridgeAddr.String(), Netmask: bridgeMask},
			MAC:    lt.MAC{Address: bridgeMAC},
		}
		network, err = libvirtc.NetworkDefineXML(string(n.XML()))
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
	// We need to explicitly assign the MAC address to avoid it changing to a lower value
	// See: https://github.com/flynn/flynn/issues/223
	bridge, err := net.InterfaceByName(bridgeName)
	if err != nil {
		return nil, err
	}
	if err := netlink.NetworkSetMacAddress(bridge, bridgeMAC); err != nil {
		return nil, err
	}
	if err := netlink.NetworkLinkUp(bridge); err != nil {
		return nil, err
	}

	iptables.RemoveExistingChain("FLYNN", bridgeName)
	chain, err := iptables.NewChain("FLYNN", bridgeName)
	if err != nil {
		return nil, err
	}
	if err := ioutil.WriteFile("/proc/sys/net/ipv4/conf/"+bridgeName+"/route_localnet", []byte("1"), 0666); err != nil {
		return nil, err
	}
	return &LibvirtLXCBackend{
		LogPath:    logPath,
		VolPath:    volPath,
		InitPath:   initPath,
		libvirt:    libvirtc,
		state:      state,
		ports:      portAlloc,
		pinkerton:  pinkertonCtx,
		forwarder:  ports.NewForwarder(net.ParseIP("0.0.0.0"), chain),
		logs:       make(map[string]*logbuf.Log),
		containers: make(map[string]*libvirtContainer),
	}, nil
}

type LibvirtLXCBackend struct {
	LogPath   string
	InitPath  string
	VolPath   string
	libvirt   libvirt.VirConnection
	state     *State
	ports     map[string]*ports.Allocator
	forwarder *ports.Forwarder
	pinkerton *pinkerton.Context

	logsMtx sync.Mutex
	logs    map[string]*logbuf.Log

	containersMtx sync.RWMutex
	containers    map[string]*libvirtContainer
}

type libvirtContainer struct {
	RootPath string
	IP       net.IP
	job      *host.Job
	l        *LibvirtLXCBackend
	done     chan struct{}
	*containerinit.Client
}

const dockerBase = "/var/lib/docker"

type dockerImageConfig struct {
	User       string
	Env        []string
	Cmd        []string
	Entrypoint []string
	WorkingDir string
	Volumes    map[string]struct{}
}

// writeResolvConf copies /etc/resolv.conf to the given path, removing any IPV6
// nameservers in the process (as IPV6 routing is currently not supported).
func writeResolvConf(path string) error {
	// do nothing if the file exists
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	file, err := os.Open("/etc/resolv.conf")
	if err != nil {
		return err
	}
	defer file.Close()
	var buf bytes.Buffer
	s := bufio.NewScanner(file)
	for s.Scan() {
		line := strings.Split(s.Text(), " ")
		if len(line) > 0 && line[0] == "nameserver" && isIPv6(line[1]) {
			continue
		}
		buf.Write(s.Bytes())
		buf.WriteByte('\n')
	}
	if err := ioutil.WriteFile(path, buf.Bytes(), 0644); err != nil {
		return err
	}
	return nil
}

func isIPv6(s string) bool {
	ip := net.ParseIP(s)
	return ip != nil && ip.To4() == nil
}

func writeContainerEnv(path string, envs ...map[string]string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	var length int
	for _, e := range envs {
		length += len(e)
	}
	data := make([]string, 0, length)

	for _, e := range envs {
		for k, v := range e {
			data = append(data, k+"="+v)
		}
	}

	return json.NewEncoder(f).Encode(data)
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
	f, err := os.Open(filepath.Join(dockerBase, "graph", id, "json"))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if err := json.NewDecoder(f).Decode(res); err != nil {
		return nil, err
	}
	return &res.Config, nil
}

func (l *LibvirtLXCBackend) Run(job *host.Job) (err error) {
	g := grohl.NewContext(grohl.Data{"backend": "libvirt-lxc", "fn": "run", "job.id": job.ID})
	g.Log(grohl.Data{"at": "start", "job.artifact.uri": job.Artifact.URI, "job.cmd": job.Config.Cmd})

	container := &libvirtContainer{
		l:    l,
		job:  job,
		done: make(chan struct{}),
	}
	if !job.Config.HostNetwork {
		container.IP, err = ipallocator.RequestIP(bridgeNet, nil)
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
	if err := bindMount("/etc/flynn/resolv.conf", filepath.Join(rootPath, "etc/resolv.conf"), false, true); err != nil {
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

	if job.Config.Env == nil {
		job.Config.Env = make(map[string]string)
	}
	if !job.Config.HostNetwork {
		for i, p := range job.Config.Ports {
			if p.Proto != "tcp" && p.Proto != "udp" {
				return fmt.Errorf("unknown port proto %q", p.Proto)
			}

			if 0 < p.RangeEnd && p.RangeEnd < p.Port {
				return fmt.Errorf("port range end %d cannot be less than port %d", p.RangeEnd, p.Port)
			}

			var port uint16
			if p.Port <= 0 {
				job.Config.Ports[i].RangeEnd = 0
				port, err = l.ports[p.Proto].Get()
			} else if p.RangeEnd > p.Port {
				for j := p.RangeEnd; j >= p.Port; j-- {
					port, err = l.ports[p.Proto].GetPort(uint16(j))
					if err != nil {
						break
					}
				}
			} else {
				port, err = l.ports[p.Proto].GetPort(uint16(p.Port))
			}
			if err != nil {
				g.Log(grohl.Data{"at": "alloc_port", "status": "error", "err": err})
				return err
			}
			job.Config.Ports[i].Port = int(port)
			if job.Config.Ports[i].RangeEnd == 0 {
				job.Config.Ports[i].RangeEnd = int(port)
			}

			if i == 0 {
				job.Config.Env["PORT"] = strconv.Itoa(int(port))
			}
			job.Config.Env[fmt.Sprintf("PORT_%d", i)] = strconv.Itoa(int(port))
		}
	}

	g.Log(grohl.Data{"at": "write_env"})
	err = writeContainerEnv(filepath.Join(rootPath, ".containerenv"),
		map[string]string{
			"PATH": "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
			"TERM": "xterm",
			"HOME": "/",
		},
		job.Config.Env,
		map[string]string{
			"HOSTNAME": job.ID,
		},
	)
	if err != nil {
		g.Log(grohl.Data{"at": "write_env", "status": "error", "err": err})
		return err
	}

	var args []string
	if !job.Config.HostNetwork {
		args = append(args,
			"-i", container.IP.String()+"/24",
			"-g", bridgeAddr.String(),
		)
	}
	if job.Config.TTY {
		args = append(args, "-tty")
	}
	if job.Config.Stdin {
		args = append(args, "-stdin")
	}
	if job.Config.WorkingDir != "" {
		args = append(args, "-w", job.Config.WorkingDir)
	} else if imageConfig.WorkingDir != "" {
		args = append(args, "-w", imageConfig.WorkingDir)
	}
	if job.Config.Uid > 0 {
		args = append(args, "-u", strconv.Itoa(job.Config.Uid))
	} else if imageConfig.User != "" {
		// TODO: check and lookup user from image config
	}
	if len(job.Config.Entrypoint) > 0 {
		args = append(args, job.Config.Entrypoint...)
		args = append(args, job.Config.Cmd...)
	} else {
		args = append(args, imageConfig.Entrypoint...)
		if len(job.Config.Cmd) > 0 {
			args = append(args, job.Config.Cmd...)
		} else {
			args = append(args, imageConfig.Cmd...)
		}
	}

	l.state.AddJob(job, container.IP.String())
	domain := &lt.Domain{
		Type:   "lxc",
		Name:   job.ID,
		Memory: lt.UnitInt{Value: 1, Unit: "GiB"},
		VCPU:   1,
		OS: lt.OS{
			Type:     lt.OSType{Value: "exe"},
			Init:     "/.containerinit",
			InitArgs: args,
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

	if !job.Config.HostNetwork {
		domain.Devices.Interfaces = []lt.Interface{{
			Type:   "network",
			Source: lt.InterfaceSrc{Network: libvirtNetName},
		}}
	}

	g.Log(grohl.Data{"at": "define_domain"})
	vd, err := l.libvirt.DomainDefineXML(string(domain.XML()))
	if err != nil {
		g.Log(grohl.Data{"at": "define_domain", "status": "error", "err": err})
		return err
	}

	g.Log(grohl.Data{"at": "create_domain"})
	if err := vd.Create(); err != nil {
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

	if !job.Config.HostNetwork {
		if len(domain.Devices.Interfaces) == 0 || domain.Devices.Interfaces[0].Target == nil ||
			domain.Devices.Interfaces[0].Target.Dev == "" {
			err = errors.New("domain config missing interface")
			g.Log(grohl.Data{"at": "enable_hairpin", "status": "error", "err": err})
			return err
		}
		iface := domain.Devices.Interfaces[0].Target.Dev
		if err := enableHairpinMode(iface); err != nil {
			g.Log(grohl.Data{"at": "enable_hairpin", "status": "error", "err": err})
			return err
		}

		for _, p := range job.Config.Ports {
			if err := l.forwarder.Add(&net.TCPAddr{IP: container.IP, Port: p.Port}, p.RangeEnd, p.Proto); err != nil {
				g.Log(grohl.Data{"at": "forward_port", "port": p.Port, "status": "error", "err": err})
				return err
			}
		}
	}

	go container.watch(nil)

	g.Log(grohl.Data{"at": "finish"})
	return nil
}

func enableHairpinMode(iface string) error {
	return ioutil.WriteFile("/sys/class/net/"+iface+"/brport/hairpin_mode", []byte("1"), 0666)
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

	if !c.job.Config.TTY {
		g.Log(grohl.Data{"at": "get_stdout"})
		stdout, stderr, err := c.Client.GetStdout()
		if err != nil {
			g.Log(grohl.Data{"at": "get_stdout", "status": "error", "err": err.Error()})
			return err
		}
		log := c.l.openLog(c.job.ID)
		defer log.Close()
		// TODO: log errors from these
		go log.Follow(1, stdout)
		go log.Follow(2, stderr)
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
	if !c.job.Config.HostNetwork {
		for _, p := range c.job.Config.Ports {
			if err := c.l.forwarder.Remove(&net.TCPAddr{IP: c.IP, Port: p.Port}, p.RangeEnd, p.Proto); err != nil {
				g.Log(grohl.Data{"at": "iptables", "status": "error", "err": err, "port": p.Port})
			}
			if p.RangeEnd == 0 {
				p.RangeEnd = p.Port
			}
			for i := p.Port; i <= p.RangeEnd; i++ {
				c.l.ports[p.Proto].Put(uint16(i))
			}
		}
		ipallocator.ReleaseIP(bridgeNet, c.IP)
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
	var client *libvirtContainer
	if req.Stdin != nil || req.Job.Job.Config.TTY {
		client, err = l.getContainer(req.Job.Job.ID)
		if err != nil {
			return err
		}
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
				g.Log(grohl.Data{"at": "error", "job.id": id, "err": err})
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

		for _, p := range j.Job.Config.Ports {
			for i := p.Port; i <= p.RangeEnd; i++ {
				l.ports[p.Proto].GetPort(uint16(i))
			}
		}
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

func (l *LibvirtLXCBackend) pinkertonPull(url string) ([]pinkerton.LayerPullInfo, error) {
	var layers []pinkerton.LayerPullInfo
	info := make(chan pinkerton.LayerPullInfo)
	done := make(chan struct{})
	go func() {
		for l := range info {
			layers = append(layers, l)
		}
		close(done)
	}()
	if err := l.pinkerton.Pull(url, info); err != nil {
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
