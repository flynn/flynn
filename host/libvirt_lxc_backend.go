// +build !dockeronly

package main

import (
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
	"sync"
	"syscall"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/alexzorin/libvirt-go"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/docker/docker/daemon/networkdriver/ipallocator"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/docker/docker/pkg/term"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/lumberjack"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/technoweenie/grohl"
	"github.com/flynn/flynn/host/containerinit"
	lt "github.com/flynn/flynn/host/libvirt"
	"github.com/flynn/flynn/host/logbuf"
	"github.com/flynn/flynn/host/pinkerton"
	"github.com/flynn/flynn/host/ports"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/pkg/cluster"
	"github.com/flynn/flynn/pkg/iptables"
)

func NewLibvirtLXCBackend(state *State, portAlloc map[string]*ports.Allocator, volPath, logPath, initPath string) (Backend, error) {
	libvirtc, err := libvirt.NewVirConnection("lxc:///")
	if err != nil {
		return nil, err
	}

	iptables.RemoveExistingChain("FLYNN", "virbr0")
	chain, err := iptables.NewChain("FLYNN", "virbr0")
	if err != nil {
		return nil, err
	}
	if err := ioutil.WriteFile("/proc/sys/net/ipv4/conf/virbr0/route_localnet", []byte("1"), 0666); err != nil {
		return nil, err
	}
	if err := ioutil.WriteFile("/sys/class/net/virbr0/bridge/stp_state", []byte("0"), 0666); err != nil {
		return nil, err
	}

	return &LibvirtLXCBackend{
		LogPath:    logPath,
		VolPath:    volPath,
		InitPath:   initPath,
		libvirt:    libvirtc,
		state:      state,
		ports:      portAlloc,
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

// TODO: read these from a configurable libvirt network
var defaultGW, defaultNet, _ = net.ParseCIDR("192.168.122.1/24")

const dockerBase = "/var/lib/docker"

type dockerImageConfig struct {
	User       string
	Env        []string
	Cmd        []string
	Entrypoint []string
	WorkingDir string
	Volumes    map[string]struct{}
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

	ip, err := ipallocator.RequestIP(defaultNet, nil)
	if err != nil {
		g.Log(grohl.Data{"at": "request_ip", "status": "error", "err": err})
		return err
	}
	container := &libvirtContainer{
		l:    l,
		job:  job,
		IP:   *ip,
		done: make(chan struct{}),
	}
	defer func() {
		if err != nil {
			go container.cleanup()
		}
	}()

	g.Log(grohl.Data{"at": "pull_image"})
	layers, err := pinkerton.Pull(job.Artifact.URI)
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
	rootPath, err := pinkerton.Checkout(job.ID, imageID)
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
	if err := bindMount("/etc/resolv.conf", filepath.Join(rootPath, "etc/resolv.conf"), false, true); err != nil {
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
	for i, p := range job.Config.Ports {
		if p.Proto != "tcp" && p.Proto != "udp" {
			return fmt.Errorf("unknown port proto %q", p.Proto)
		}

		var port uint16
		if p.Port <= 0 {
			job.Config.Ports[i].RangeEnd = 0
			port, err = l.ports[p.Proto].Get()
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

	args := []string{
		"-i", ip.String() + "/24",
		"-g", defaultGW.String(),
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

	l.state.AddJob(job)
	l.state.SetInternalIP(job.ID, ip.String())
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
			Interfaces: []lt.Interface{{
				Type:   "network",
				Source: lt.InterfaceSrc{Network: "default"},
			}},
			Consoles: []lt.Console{{Type: "pty"}},
		},
		OnPoweroff: "preserve",
		OnCrash:    "preserve",
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
		if err := l.forwarder.Add(&net.TCPAddr{IP: *ip, Port: p.Port}, p.RangeEnd, p.Proto); err != nil {
			g.Log(grohl.Data{"at": "forward_port", "port": p.Port, "status": "error", "err": err})
			return err
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
		l.logs[id] = logbuf.NewLog(&lumberjack.Logger{Dir: filepath.Join(l.LogPath, id)})
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
	for startTime := time.Now(); time.Since(startTime) < 5*time.Second; time.Sleep(time.Millisecond) {
		if !symlinked {
			// We can't connect to the socket file directly because
			// the path to it is longer than 108 characters (UNIX_PATH_MAX).
			// Create a temporary symlink to connect to.
			if err = os.Symlink(socketPath, symlink); err != nil {
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
		g.Log(grohl.Data{"at": "connect", "status": "error", "err": err})
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
		go log.ReadFrom(1, stdout)
		go log.ReadFrom(2, stderr)
	}

	g.Log(grohl.Data{"at": "watch_changes"})
	for change := range c.Client.StreamState() {
		g.Log(grohl.Data{"at": "change", "state": change.State.String()})
		if change.Error != "" {
			err := errors.New(change.Error)
			g.Log(grohl.Data{"at": "change", "status": "error", "err": err})
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
	if err := pinkerton.Cleanup(c.job.ID); err != nil {
		g.Log(grohl.Data{"at": "pinkerton", "status": "error", "err": err})
	}
	for _, m := range c.job.Config.Mounts {
		if err := syscall.Unmount(filepath.Join(c.RootPath, m.Location), 0); err != nil {
			g.Log(grohl.Data{"at": "unmount", "location": m.Location, "status": "error", "err": err})
		}
	}
	for _, p := range c.job.Config.Ports {
		if err := c.l.forwarder.Remove(&net.TCPAddr{IP: c.IP, Port: p.Port}, p.RangeEnd, p.Proto); err != nil {
			g.Log(grohl.Data{"at": "iptables", "status": "error", "err": err, "port": p.Port})
		}
		c.l.ports[p.Proto].Put(uint16(p.Port))
	}
	ipallocator.ReleaseIP(defaultNet, &c.IP)
	g.Log(grohl.Data{"at": "finish"})
	return nil
}

func (l *LibvirtLXCBackend) Stop(id string) error {
	// TODO: follow up with sigkill
	return l.Signal(id, int(syscall.SIGTERM))
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
		if req.Job.Job.Config.TTY || req.Stream && err == io.EOF {
			<-client.done
			job := l.state.GetJob(req.Job.Job.ID)
			if job.Status == host.StatusDone || job.Status == host.StatusCrashed {
				err = ExitError(job.ExitStatus)
			}
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

	log := l.openLog(req.Job.Job.ID)
	r := log.NewReader()
	defer r.Close()
	if !req.Logs {
		if err := r.SeekToEnd(); err != nil {
			return err
		}
	}

	if req.Attached != nil {
		req.Attached <- struct{}{}
	}

	for {
		data, err := r.ReadData(req.Stream)
		if err != nil {
			return err
		}
		switch data.Stream {
		case 1:
			if req.Stdout == nil {
				continue
			}
			if _, err := req.Stdout.Write([]byte(data.Message)); err != nil {
				return err
			}
		case 2:
			if req.Stderr == nil {
				continue
			}
			if _, err := req.Stderr.Write([]byte(data.Message)); err != nil {
				return err
			}
		}
	}
}

func (l *LibvirtLXCBackend) Cleanup() error {
	l.containersMtx.Lock()
	ids := make([]string, 0, len(l.containers))
	chans := make([]chan struct{}, 0, len(l.containers))
	for id, c := range l.containers {
		ids = append(ids, id)
		chans = append(chans, c.done)
	}
	l.containersMtx.Unlock()
	for _, id := range ids {
		if err := l.Stop(id); err != nil {
			return err
		}
	}
	for _, done := range chans {
		<-done
	}
	return nil
}

func (l *LibvirtLXCBackend) RestoreState(jobs map[string]*host.ActiveJob, dec *json.Decoder) error {
	containers := make(map[string]*libvirtContainer)
	if err := dec.Decode(&containers); err != nil {
		return err
	}
	for _, j := range jobs {
		container, ok := containers[j.Job.ID]
		if !ok {
			continue
		}
		container.l = l
		container.job = j.Job
		container.done = make(chan struct{})
		status := make(chan error)
		go container.watch(status)
		if err := <-status; err != nil {
			// log error
			l.state.RemoveJob(j.Job.ID)
			container.cleanup()
			continue
		}
		l.containers[j.Job.ID] = container

		for _, p := range j.Job.Config.Ports {
			l.ports[p.Proto].GetPort(uint16(p.Port))
		}

	}
	return nil
}

func (l *LibvirtLXCBackend) SaveState(e *json.Encoder) error {
	l.containersMtx.RLock()
	defer l.containersMtx.RUnlock()
	return e.Encode(l.containers)
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
