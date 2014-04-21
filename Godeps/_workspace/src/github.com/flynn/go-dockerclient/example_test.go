// Copyright 2013 go-dockerclient authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package docker_test

import (
	"bytes"
	"github.com/flynn/go-dockerclient"
	"log"
)

func ExampleAttachToContainer() {
	client, err := docker.NewClient("http://localhost:4243")
	if err != nil {
		log.Fatal(err)
	}
	// Reading logs from container a84849 and sending them to buf.
	var buf bytes.Buffer
	err = client.AttachToContainer(docker.AttachToContainerOptions{
		Container:    "a84849",
		OutputStream: &buf,
		Logs:         true,
		Stdout:       true,
		Stderr:       true,
	})
	if err != nil {
		log.Fatal(err)
	}
	log.Println(buf.String())
	// Attaching to stdout and streaming.
	buf.Reset()
	err = client.AttachToContainer(docker.AttachToContainerOptions{
		Container:    "a84849",
		OutputStream: &buf,
		Stdout:       true,
		Stream:       true,
	})
	if err != nil {
		log.Fatal(err)
	}
	log.Println(buf.String())
}
