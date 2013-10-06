package main

import (
	"encoding/gob"
	"log"
	"net"

	"github.com/flynn/lorne/types"
	"github.com/titanous/go-dockerclient"
)

func attachServer() {
	l, err := net.Listen("tcp", ":1120")
	if err != nil {
		log.Fatal(err)
	}
	for {
		conn, err := l.Accept()
		if err != nil {
			log.Println("attach accept error:", err)
		}
		go attachHandler(conn)
	}
}

func attachHandler(conn net.Conn) {
	defer conn.Close()

	var req lorne.AttachReq
	if err := gob.NewDecoder(conn).Decode(&req); err != nil {
		log.Println("gob decode error:", err)
		return
	}

	log.Println("attaching job", req.JobID)
	job := state.GetJob(req.JobID)
	if job.Job == nil || job.Status != lorne.StatusRunning {
		return
	}

	opts := docker.AttachToContainerOptions{
		Container:    job.ContainerID,
		InputStream:  conn,
		OutputStream: conn,
		Stdin:        req.Flags&lorne.AttachFlagStdin != 0,
		Stdout:       req.Flags&lorne.AttachFlagStdout != 0,
		Stderr:       req.Flags&lorne.AttachFlagStderr != 0,
		Logs:         req.Flags&lorne.AttachFlagLogs != 0,
		Stream:       req.Flags&lorne.AttachFlagStream != 0,
	}
	if err := Docker.AttachToContainer(opts); err != nil {
		log.Println("attach error:", err)
		return
	}
}
