package main

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"flag"
	"io"
	"log"
	"os"
	"strconv"

	"github.com/flynn/go-discover/discover"
	"github.com/flynn/lorne/types"
	sampic "github.com/flynn/sampi/client"
	"github.com/flynn/sampi/types"
	"github.com/titanous/go-dockerclient"
)

var state = NewState()
var Docker *docker.Client

func main() {
	externalAddr := flag.String("external", "", "external IP of host")
	configFile := flag.String("config", "", "configuration file")
	hostID := flag.String("id", randomID(), "host id")
	flag.Parse()

	go server()
	go allocatePorts()

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
	log.Print("Connected to scheduler")

	Docker, err = docker.NewClient("http://localhost:4243")
	if err != nil {
		log.Fatal(err)
	}

	go streamEvents(Docker)
	go syncScheduler(scheduler)

	var host *sampi.Host
	if *configFile != "" {
		host, err = parseConfig(*configFile)
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
	log.Print("Host registered")
	processJobs(jobs, *externalAddr)
}

func processJobs(jobs chan *sampi.Job, externalAddr string) {
	for job := range jobs {
		log.Printf("%#v", job.Config)
		var hostConfig *docker.HostConfig
		if job.TCPPorts > 0 {
			port := strconv.Itoa(<-portAllocator)
			job.Config.Env = append(job.Config.Env, "PORT="+port)
			job.Config.ExposedPorts = map[string]struct{}{port + "/tcp": struct{}{}}
			job.Config.Name = "flynn-" + job.ID
			hostConfig = &docker.HostConfig{
				PortBindings:    map[string][]docker.PortBinding{port + "/tcp": {{HostPort: port}}},
				PublishAllPorts: true,
			}
		}
		if externalAddr != "" {
			job.Config.Env = append(job.Config.Env, "EXTERNAL_IP="+externalAddr, "SD_HOST="+externalAddr, "DISCOVERD="+externalAddr+":1111")
		}
		state.AddJob(job)
		container, err := Docker.CreateContainer(job.Config)
		if err == docker.ErrNoSuchImage {
			err = Docker.PullImage(docker.PullImageOptions{Repository: job.Config.Image}, os.Stdout)
			if err != nil {
				log.Fatal(err)
			}
			container, err = Docker.CreateContainer(job.Config)
		}
		if err != nil {
			log.Fatal(err)
		}
		state.SetContainerID(job.ID, container.ID)
		state.WaitAttach(job.ID)
		if err := Docker.StartContainer(container.ID, hostConfig); err != nil {
			log.Fatal(err)
		}
		state.SetStatusRunning(job.ID)
	}
}

func syncScheduler(scheduler *sampic.Client) {
	events := make(chan lorne.Event)
	state.AddListener("all", events)
	for event := range events {
		if event.Event != "stop" {
			continue
		}
		log.Println("remove job", event.JobID)
		if err := scheduler.RemoveJobs([]string{event.JobID}); err != nil {
			log.Println("remove job", event.JobID, "error:", err)
			// TODO: try to reconnect?
		}
	}
}

func streamEvents(client *docker.Client) {
	stream, err := client.Events()
	if err != nil {
		log.Fatal(err)
	}
	for event := range stream.Events {
		log.Printf("%#v", event)
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
	log.Println("events done", stream.Error)
}

func randomID() string {
	b := make([]byte, 16)
	enc := make([]byte, 24)
	_, err := io.ReadFull(rand.Reader, b)
	if err != nil {
		panic(err) // This shouldn't ever happen, right?
	}
	base64.URLEncoding.Encode(enc, b)
	return string(bytes.TrimRight(enc, "="))
}

var portAllocator = make(chan int)

// TODO: fix this, horribly broken
const startPort = 55000
const endPort = 65535

func allocatePorts() {
	for i := startPort; i < endPort; i++ {
		portAllocator <- i
	}
	// TODO: handle wrap-around
}
