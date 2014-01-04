package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/flynn/go-discoverd"
	"github.com/flynn/go-dockerclient"
	"github.com/flynn/lorne/types"
	sampic "github.com/flynn/sampi/client"
	"github.com/flynn/sampi/types"
	"github.com/technoweenie/grohl"
)

func main() {
	externalAddr := flag.String("external", "", "external IP of host")
	configFile := flag.String("config", "", "configuration file")
	manifestFile := flag.String("manifest", "", "manifest file")
	hostID := flag.String("id", "host1", "host id")
	flag.Parse()
	grohl.AddContext("app", "lorne")
	grohl.Log(grohl.Data{"at": "start"})
	g := grohl.NewContext(grohl.Data{"fn": "main"})

	dockerc, err := docker.NewClient("unix:///var/run/docker.sock")
	if err != nil {
		log.Fatal(err)
	}

	state := NewState()
	ports := make(chan int)

	go allocatePorts(ports, 55000, 65535)
	go serveHTTP(&Host{state: state, docker: dockerc}, &attachHandler{state: state, docker: dockerc})
	go streamEvents(dockerc, state)

	processor := &jobProcessor{
		externalAddr: *externalAddr,
		docker:       dockerc,
		state:        state,
		discoverd:    os.Getenv("DISCOVERD"),
	}

	runner := &manifestRunner{
		env:        parseEnviron(),
		externalIP: *externalAddr,
		ports:      ports,
		processor:  processor,
		docker:     dockerc,
	}

	var disc *discoverd.Client
	if *manifestFile != "" {
		f, err := os.Open(*manifestFile)
		if err != nil {
			log.Fatal(err)
		}
		services, err := runner.runManifest(f)
		if err != nil {
			log.Fatal(err)
		}
		f.Close()

		if d, ok := services["discoverd"]; ok {
			processor.discoverd = fmt.Sprintf("%s:%d", d.InternalIP, d.TCPPorts[0])
			disc, err = discoverd.NewClientUsingAddress(processor.discoverd)
			if err != nil {
				log.Fatal(err)
			}
		}
	}

	if processor.discoverd == "" && *externalAddr != "" {
		processor.discoverd = *externalAddr + ":1111"
	}
	// HACK: use env as global for discoverd connection in sampic
	os.Setenv("DISCOVERD", processor.discoverd)
	if disc == nil {
		disc, err = discoverd.NewClientUsingAddress(processor.discoverd)
		if err != nil {
			log.Fatal(err)
		}
	}
	if *externalAddr != "" {
		err = disc.RegisterWithHost("flynn-lorne."+*hostID, *externalAddr, "1113", nil)
	} else {
		err = disc.Register("flynn-lorne."+*hostID, "1113", nil)
	}
	if err != nil {
		log.Fatal(err)
	}

	scheduler, err := sampic.New()
	if err != nil {
		log.Fatal(err)
	}
	g.Log(grohl.Data{"at": "sampi_connected"})

	events := make(chan lorne.Event)
	state.AddListener("all", events)
	go syncScheduler(scheduler, events)

	var host *sampi.Host
	if *configFile != "" {
		host, err = openConfig(*configFile)
		if err != nil {
			log.Fatal(err)
		}
	} else {
		host = &sampi.Host{Resources: make(map[string]sampi.ResourceValue)}
	}
	if _, ok := host.Resources["memory"]; !ok {
		host.Resources["memory"] = sampi.ResourceValue{Value: 1024}
	}
	host.ID = *hostID

	jobs := make(chan *sampi.Job)
	scheduler.RegisterHost(host, jobs)
	g.Log(grohl.Data{"at": "host_registered"})
	processor.Process(ports, jobs)
}

type jobProcessor struct {
	externalAddr string
	discoverd    string
	docker       interface {
		CreateContainer(*docker.Config) (*docker.Container, error)
		PullImage(docker.PullImageOptions, io.Writer) error
		StartContainer(string, *docker.HostConfig) error
	}
	state *State
}

func (p *jobProcessor) Process(ports <-chan int, jobs chan *sampi.Job) {
	for job := range jobs {
		p.processJob(ports, job)
	}
}

func (p *jobProcessor) processJob(ports <-chan int, job *sampi.Job) (*docker.Container, error) {
	g := grohl.NewContext(grohl.Data{"fn": "process_job", "job.id": job.ID})
	g.Log(grohl.Data{"at": "start", "job.image": job.Config.Image, "job.cmd": job.Config.Cmd, "job.entrypoint": job.Config.Entrypoint})

	var hostConfig *docker.HostConfig
	if job.TCPPorts > 0 {
		port := strconv.Itoa(<-ports)
		job.Config.Env = append(job.Config.Env, "PORT="+port)
		job.Config.ExposedPorts = map[string]struct{}{port + "/tcp": struct{}{}}
		hostConfig = &docker.HostConfig{
			PortBindings:    map[string][]docker.PortBinding{port + "/tcp": {{HostPort: port}}},
			PublishAllPorts: true,
		}
	}
	if p.externalAddr != "" {
		job.Config.Env = appendUnique(job.Config.Env, "EXTERNAL_IP="+p.externalAddr, "SD_HOST="+p.externalAddr, "DISCOVERD="+p.discoverd)
	}
	p.state.AddJob(job)
	g.Log(grohl.Data{"at": "create_container"})
	container, err := p.docker.CreateContainer(job.Config)
	if err == docker.ErrNoSuchImage {
		g.Log(grohl.Data{"at": "pull_image"})
		err = p.docker.PullImage(docker.PullImageOptions{Repository: job.Config.Image}, os.Stdout)
		if err != nil {
			g.Log(grohl.Data{"at": "pull_image", "status": "error", "err": err})
			p.state.SetStatusFailed(job.ID, err)
			return nil, err
		}
		container, err = p.docker.CreateContainer(job.Config)
	}
	if err != nil {
		g.Log(grohl.Data{"at": "create_container", "status": "error", "err": err})
		p.state.SetStatusFailed(job.ID, err)
		return nil, err
	}
	p.state.SetContainerID(job.ID, container.ID)
	p.state.WaitAttach(job.ID)
	g.Log(grohl.Data{"at": "start_container"})
	if err := p.docker.StartContainer(container.ID, hostConfig); err != nil {
		g.Log(grohl.Data{"at": "start_container", "status": "error", "err": err})
		p.state.SetStatusFailed(job.ID, err)
		return nil, err
	}
	p.state.SetStatusRunning(job.ID)
	g.Log(grohl.Data{"at": "finish"})
	return container, nil
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

type sampiSyncClient interface {
	RemoveJobs([]string) error
}

func syncScheduler(scheduler sampiSyncClient, events <-chan lorne.Event) {
	for event := range events {
		if event.Event != "stop" {
			continue
		}
		grohl.Log(grohl.Data{"fn": "scheduler_event", "at": "remove_job", "job.id": event.JobID})
		if err := scheduler.RemoveJobs([]string{event.JobID}); err != nil {
			grohl.Log(grohl.Data{"fn": "scheduler_event", "at": "remove_job", "status": "error", "err": err, "job.id": event.JobID})
			// TODO: try to reconnect?
		}
	}
}

type dockerStreamClient interface {
	Events() (*docker.EventStream, error)
	InspectContainer(string) (*docker.Container, error)
}

func streamEvents(client dockerStreamClient, state *State) {
	stream, err := client.Events()
	if err != nil {
		log.Fatal(err)
	}
	for event := range stream.Events {
		if event.Status != "die" {
			continue
		}
		container, err := client.InspectContainer(event.ID)
		if err != nil {
			log.Println("inspect container", event.ID, "error:", err)
			// TODO: set job status anyway?
			continue
		}
		state.SetStatusDone(event.ID, container.State.ExitCode)
	}
}

// TODO: fix this, horribly broken

func allocatePorts(ports chan<- int, startPort, endPort int) {
	for i := startPort; i < endPort; i++ {
		ports <- i
	}
	// TODO: handle wrap-around
}
