package main

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"io"
	"log"
	"os"

	"github.com/flynn/rpcplus"
	"github.com/flynn/sampi/types"
	"github.com/titanous/go-dockerclient"
)

func main() {
	scheduler, err := rpcplus.DialHTTP("tcp", "localhost:1112")
	if err != nil {
		log.Fatal(err)
	}
	log.Print("Connected to scheduler")

	client, err := docker.NewClient("http://localhost:4243")
	if err != nil {
		log.Fatal(err)
	}

	// register host data
	host := types.Host{
		ID:        randomID(),
		Resources: map[string]types.ResourceValue{"memory": types.ResourceValue{Value: 1024}},
	}

	// TODO: track job state

	jobs := make(chan *types.Job)
	scheduler.StreamGo("Scheduler.RegisterHost", host, jobs)
	log.Print("Host registered")
	for job := range jobs {
		log.Printf("%#v", job.Config)
		err := client.PullImage(docker.PullImageOptions{Repository: job.Config.Image}, os.Stdout)
		if err != nil {
			log.Fatal(err)
		}
		container, err := client.CreateContainer(job.Config)
		if err != nil {
			log.Fatal(err)
		}
		if err := client.StartContainer(container.ID); err != nil {
			log.Fatal(err)
		}
	}
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
