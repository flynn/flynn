package main

import (
	"flag"
	"io"
	"log"
	"os"
	"strconv"

	"github.com/flynn/go-discover/discover"
	"github.com/flynn/go-dockerclient"
	"github.com/flynn/lorne/types"
	sampic "github.com/flynn/sampi/client"
	"github.com/flynn/sampi/types"
	"github.com/technoweenie/grohl"
)

func main() {
	externalAddr := flag.String("external", "", "external IP of host")
	configFile := flag.String("config", "", "configuration file")
	hostID := flag.String("id", "host1", "host id")
	flag.Parse()
	grohl.AddContext("app", "lorne")
	grohl.Log(grohl.Data{"at": "start"})
	g := grohl.NewContext(grohl.Data{"fn": "main"})

	disc, err := discover.NewClient()
	if err != nil {
		log.Fatal(err)
	}
	if err := disc.Register("flynn-lorne."+*hostID, "1113", nil); err != nil {
		log.Fatal(err)
	}

	scheduler, err := sampic.New()
	if err != nil {
		log.Fatal(err)
	}
	g.Log(grohl.Data{"at": "sampi_connected"})

	dockerc, err := docker.NewClient("http://localhost:4243")
	if err != nil {
		log.Fatal(err)
	}

	state := NewState()
	ports := make(chan int)

	go allocatePorts(ports, 55000, 65535)
	go serveHTTP(&Host{state: state, docker: dockerc}, &attachHandler{state: state, docker: dockerc})
	go streamEvents(dockerc, state)

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
	(&jobProcessor{
		externalAddr: *externalAddr,
		docker:       dockerc,
		state:        state,
		ports:        ports,
	}).Process(jobs)
}

type jobProcessor struct {
	externalAddr string
	docker       interface {
		CreateContainer(*docker.Config) (*docker.Container, error)
		PullImage(docker.PullImageOptions, io.Writer) error
		StartContainer(string, *docker.HostConfig) error
	}
	state *State
	ports <-chan int
}

func (p *jobProcessor) Process(jobs chan *sampi.Job) {
	for job := range jobs {
		p.processJob(job)
	}
}

func (p *jobProcessor) processJob(job *sampi.Job) {
	g := grohl.NewContext(grohl.Data{"fn": "process_job", "job.id": job.ID})
	g.Log(grohl.Data{"at": "start", "job.image": job.Config.Image, "job.cmd": job.Config.Cmd, "job.entrypoint": job.Config.Entrypoint})

	var hostConfig *docker.HostConfig
	if job.TCPPorts > 0 {
		port := strconv.Itoa(<-p.ports)
		job.Config.Env = append(job.Config.Env, "PORT="+port)
		job.Config.ExposedPorts = map[string]struct{}{port + "/tcp": struct{}{}}
		hostConfig = &docker.HostConfig{
			PortBindings:    map[string][]docker.PortBinding{port + "/tcp": {{HostPort: port}}},
			PublishAllPorts: true,
		}
	}
	if p.externalAddr != "" {
		job.Config.Env = append(job.Config.Env, "EXTERNAL_IP="+p.externalAddr, "SD_HOST="+p.externalAddr, "DISCOVERD="+p.externalAddr+":1111")
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
			return
		}
		container, err = p.docker.CreateContainer(job.Config)
	}
	if err != nil {
		g.Log(grohl.Data{"at": "create_container", "status": "error", "err": err})
		p.state.SetStatusFailed(job.ID, err)
		return
	}
	p.state.SetContainerID(job.ID, container.ID)
	p.state.WaitAttach(job.ID)
	g.Log(grohl.Data{"at": "start_container"})
	if err := p.docker.StartContainer(container.ID, hostConfig); err != nil {
		g.Log(grohl.Data{"at": "start_container", "status": "error", "err": err})
		p.state.SetStatusFailed(job.ID, err)
		return
	}
	p.state.SetStatusRunning(job.ID)
	g.Log(grohl.Data{"at": "finish"})
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
