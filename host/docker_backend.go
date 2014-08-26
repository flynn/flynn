package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/fsouza/go-dockerclient"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/technoweenie/grohl"
	"github.com/flynn/flynn/host/ports"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/pkg/demultiplex"
)

func NewDockerBackend(state *State, portAlloc map[string]*ports.Allocator, bindAddr string) (Backend, error) {
	dockerc, err := docker.NewClient("unix:///var/run/docker.sock")
	if err != nil {
		return nil, err
	}
	d := &DockerBackend{
		state:    state,
		ports:    portAlloc,
		docker:   dockerc,
		bindAddr: bindAddr,
	}
	go d.handleEvents()
	return d, nil
}

type DockerBackend struct {
	docker dockerClient
	state  *State
	ports  map[string]*ports.Allocator

	bindAddr string
}

type dockerClient interface {
	PullImage(docker.PullImageOptions, docker.AuthConfiguration) error
	CreateContainer(docker.CreateContainerOptions) (*docker.Container, error)
	StartContainer(string, *docker.HostConfig) error
	InspectContainer(string) (*docker.Container, error)
	AddEventListener(chan<- *docker.APIEvents) error
	RemoveEventListener(chan *docker.APIEvents) error
	StopContainer(string, uint) error
	ResizeContainerTTY(string, int, int) error
	AttachToContainer(docker.AttachToContainerOptions) error
	KillContainer(docker.KillContainerOptions) error
	ListContainers(docker.ListContainersOptions) ([]docker.APIContainers, error)
}

func (d *DockerBackend) Run(job *host.Job) error {
	g := grohl.NewContext(grohl.Data{"backend": "docker", "fn": "run", "job.id": job.ID})
	g.Log(grohl.Data{"at": "start", "job.artifact.uri": job.Artifact.URI, "job.cmd": job.Config.Cmd})

	image, pullOpts, err := parseDockerImageURI(job.Artifact.URI)
	if err != nil {
		g.Log(grohl.Data{"at": "parse_artifact_uri", "status": "error", "err": err})
		return err
	}

	config := &docker.Config{
		Image:        image,
		Entrypoint:   job.Config.Entrypoint,
		Cmd:          job.Config.Cmd,
		Tty:          job.Config.TTY,
		AttachStdout: true,
		AttachStderr: true,
		AttachStdin:  job.Config.Stdin,
		StdinOnce:    job.Config.Stdin,
		OpenStdin:    job.Config.Stdin,
		ExposedPorts: make(map[docker.Port]struct{}, len(job.Config.Ports)),
		Env:          make([]string, 0, len(job.Config.Env)+len(job.Config.Ports)+1),
		Volumes:      make(map[string]struct{}, len(job.Config.Mounts)),
	}
	opts := docker.CreateContainerOptions{Config: config}
	hostConfig := &docker.HostConfig{
		PortBindings: make(map[docker.Port][]docker.PortBinding, len(job.Config.Ports)),
	}
	for k, v := range job.Config.Env {
		config.Env = append(config.Env, k+"="+v)
	}

	for i, p := range job.Config.Ports {
		if p.Port == 0 {
			port, err := d.ports[p.Proto].Get()
			if err != nil {
				return err
			}
			p.Port = int(port)
		}
		port := strconv.Itoa(p.Port)

		if i == 0 {
			config.Env = append(config.Env, "PORT="+port)
		}
		config.Env = append(config.Env, fmt.Sprintf("PORT_%d=%s", i, port))
		config.ExposedPorts[docker.Port(port+"/"+p.Proto)] = struct{}{}
		hostConfig.PortBindings[docker.Port(port+"/"+p.Proto)] = []docker.PortBinding{{HostPort: port, HostIp: d.bindAddr}}
	}

	hostConfig.Binds = make([]string, 0, len(job.Config.Mounts))
	for _, m := range job.Config.Mounts {
		if m.Target == "" {
			config.Volumes[m.Location] = struct{}{}
		} else {
			bind := fmt.Sprintf("%s:%s:", m.Target, m.Location)
			if m.Writeable {
				bind += "rw"
			} else {
				bind += "ro"
			}
			hostConfig.Binds = append(hostConfig.Binds, bind)
		}
	}

	if strings.HasPrefix(job.ID, "flynn-") {
		opts.Name = job.ID
	} else {
		opts.Name = "flynn-" + job.ID
	}

	d.state.AddJob(job)
	g.Log(grohl.Data{"at": "create_container"})
	container, err := d.docker.CreateContainer(opts)
	if err == docker.ErrNoSuchImage {
		g.Log(grohl.Data{"at": "pull_image"})
		pullOpts.OutputStream = os.Stdout
		err = d.docker.PullImage(*pullOpts, docker.AuthConfiguration{})
		if err != nil {
			g.Log(grohl.Data{"at": "pull_image", "status": "error", "err": err})
			return err
		}
		container, err = d.docker.CreateContainer(opts)
	}
	if err != nil {
		g.Log(grohl.Data{"at": "create_container", "status": "error", "err": err})
		return err
	}
	d.state.SetContainerID(job.ID, container.ID)
	d.state.WaitAttach(job.ID)
	g.Log(grohl.Data{"at": "start_container"})
	if err := d.docker.StartContainer(container.ID, hostConfig); err != nil {
		g.Log(grohl.Data{"at": "start_container", "status": "error", "err": err})
		return err
	}
	d.state.SetStatusRunning(job.ID)
	container, err = d.docker.InspectContainer(container.ID)
	if err != nil {
		g.Log(grohl.Data{"at": "inspect_container", "status": "error", "err": err})
		return err
	}
	d.state.SetInternalIP(job.ID, container.NetworkSettings.IPAddress)
	g.Log(grohl.Data{"at": "finish"})
	return nil
}

func (d *DockerBackend) Stop(id string) error {
	const stopTimeout = 10
	return d.docker.StopContainer(d.state.GetJob(id).ContainerID, stopTimeout)
}

func (d *DockerBackend) RestoreState(jobs map[string]*host.ActiveJob, dec *json.Decoder) error {
	for id, job := range jobs {
		container, err := d.docker.InspectContainer(job.ContainerID)
		if _, ok := err.(*docker.NoSuchContainer); ok {
			delete(jobs, id)
		} else if err != nil {
			return err
		}
		if !container.State.Running {
			delete(jobs, id)
		}
	}
	return nil
}

func (d *DockerBackend) ResizeTTY(id string, height, width uint16) error {
	job := d.state.GetJob(id)
	if job == nil {
		return errors.New("unknown job")
	}
	return d.docker.ResizeContainerTTY(job.ContainerID, int(height), int(width))
}

func (d *DockerBackend) Signal(id string, sig int) error {
	job := d.state.GetJob(id)
	if job == nil {
		return errors.New("unknown job")
	}
	return d.docker.KillContainer(docker.KillContainerOptions{ID: job.ContainerID, Signal: docker.Signal(sig)})
}

func (d *DockerBackend) Attach(req *AttachRequest) error {
	outR, outW := io.Pipe()
	opts := docker.AttachToContainerOptions{
		Container:    req.Job.ContainerID,
		InputStream:  req.Stdin,
		OutputStream: outW,
		Logs:         req.Logs,
		Stream:       req.Stream,
		Success:      req.Attached,
		Stdout:       req.Stdout != nil,
		Stderr:       req.Stderr != nil,
		Stdin:        req.Stdin != nil,
	}
	if req.Job.Job.Config.TTY {
		go func() {
			io.Copy(req.Stdout, outR)
			req.Stdout.Close()
		}()
	} else if req.Stdout != nil || req.Stderr != nil {
		go func() {
			demultiplex.Copy(req.Stdout, req.Stderr, outR)
			req.Stdout.Close()
			req.Stderr.Close()
		}()
	}

	if req.Job.Job.Config.TTY && opts.Stdin {
		resize := func() { d.docker.ResizeContainerTTY(req.Job.ContainerID, int(req.Height), int(req.Width)) }
		if req.Job.Status == host.StatusRunning {
			resize()
		} else {
			var once sync.Once
			go func() {
				ch := d.state.AddListener(req.Job.Job.ID)
				defer d.state.RemoveListener(req.Job.Job.ID, ch)
				go func() {
					// There is a race that can result in the listener being
					// added after the container has started, so check the
					// status *after* subscribing.
					// This can deadlock if we try to get a state lock while an
					// event is being sent on the listen channel, so we do it
					// in the goroutine and wrap in a sync.Once.
					j := d.state.GetJob(req.Job.Job.ID)
					if j.Status == host.StatusRunning {
						once.Do(resize)
					}
				}()
				for event := range ch {
					if event.Event == "start" {
						once.Do(resize)
						return
					}
					if event.Event == "stop" {
						return
					}
				}
			}()
		}
	}

	err := d.docker.AttachToContainer(opts)
	outW.Close()
	if err != nil {
		return err
	}

	if req.Job.Job.Config.TTY || req.Stream {
		exited := make(chan struct{})
		ch := d.state.AddListener(req.Job.Job.ID)
		go func() {
			defer d.state.RemoveListener(req.Job.Job.ID, ch)
			for e := range ch {
				if e.Event == "stop" {
					close(exited)
					return
				}
			}
		}()
		job := d.state.GetJob(req.Job.Job.ID)
		if job.Status != host.StatusDone && job.Status != host.StatusCrashed {
			<-exited
			job = d.state.GetJob(req.Job.Job.ID)
		}
		return ExitError(job.ExitStatus)
	}

	return nil
}

func (d *DockerBackend) handleEvents() {
	stream := make(chan *docker.APIEvents)
	if err := d.docker.AddEventListener(stream); err != nil {
		log.Fatal(err)
	}
	defer d.docker.RemoveEventListener(stream)
	for event := range stream {
		if event.Status != "die" {
			continue
		}
		container, err := d.docker.InspectContainer(event.ID)
		if err != nil {
			log.Println("inspect container", event.ID, "error:", err)
			// TODO: set job status anyway?
			continue
		}
		// TODO: return ports to pool
		d.state.SetContainerStatusDone(event.ID, container.State.ExitCode)
	}
}

func (d *DockerBackend) Cleanup() error {
	g := grohl.NewContext(grohl.Data{"backend": "docker", "fn": "cleanup"})
	g.Log(grohl.Data{"at": "start"})
	containers, err := d.docker.ListContainers(docker.ListContainersOptions{})
	if err != nil {
		g.Log(grohl.Data{"at": "list", "status": "error", "err": err})
		return err
	}
outer:
	for _, c := range containers {
		for _, name := range c.Names {
			if strings.HasPrefix(name, "/flynn-") {
				g.Log(grohl.Data{"at": "kill", "container.id": c.ID, "container.name": name})
				if err := d.docker.KillContainer(docker.KillContainerOptions{ID: c.ID}); err != nil {
					g.Log(grohl.Data{"at": "kill", "container.id": c.ID, "container.name": name, "status": "error", "err": err})
				}
				continue outer
			}
		}
	}
	g.Log(grohl.Data{"at": "finish"})
	return nil
}

func appendUnique(s []string, vars ...string) []string {
outer:
	for _, v := range vars {
		for _, existing := range s {
			if strings.HasPrefix(existing, strings.SplitN(v, "=", 2)[0]+"=") {
				continue outer
			}
		}
		s = append(s, v)
	}
	return s
}

func parseDockerImageURI(s string) (name string, opts *docker.PullImageOptions, err error) {
	uri, err := url.Parse(s)
	if err != nil {
		return
	}
	if len(uri.Path) < 2 {
		err = fmt.Errorf("invalid image path %s", uri.Path)
		return
	}
	name = uri.Path[1:]
	q := uri.Query()
	tag := q.Get("id")
	if tag == "" {
		tag = q.Get("tag")
	}
	opts = &docker.PullImageOptions{
		Repository: name,
		Tag:        tag,
		Registry:   uri.Host,
	}
	if tag != "" {
		name += ":" + tag
	}
	return
}
