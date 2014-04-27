package main

import (
	"encoding/json"
	"io"
	"net/http"
	"sync"

	"github.com/flynn/flynn-host/types"
	"github.com/flynn/go-dockerclient"
	"github.com/technoweenie/grohl"
)

type attachHandler struct {
	state  *State
	docker dockerAttachClient
}

func (h *attachHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	var attachReq host.AttachReq
	if err := json.NewDecoder(req.Body).Decode(&attachReq); err != nil {
		http.Error(w, "invalid JSON", 400)
		return
	}
	conn, _, err := w.(http.Hijacker).Hijack()
	if err != nil {
		return
	}
	conn.Write([]byte("HTTP/1.1 200 OK\r\nContent-Type: application/vnd.flynn.attach-hijack\r\n\r\n"))
	h.attach(&attachReq, conn)
}

type dockerAttachClient interface {
	ResizeContainerTTY(string, int, int) error
	AttachToContainer(docker.AttachToContainerOptions) error
}

func (h *attachHandler) attach(req *host.AttachReq, conn io.ReadWriteCloser) {
	defer conn.Close()

	g := grohl.NewContext(grohl.Data{"fn": "attach", "job.id": req.JobID})
	g.Log(grohl.Data{"at": "start"})
	attachWait := make(chan struct{})
	job := h.state.AddAttacher(req.JobID, attachWait)
	if job == nil {
		defer h.state.RemoveAttacher(req.JobID, attachWait)
		if _, err := conn.Write([]byte{host.AttachWaiting}); err != nil {
			return
		}
		// TODO: add timeout
		<-attachWait
		job = h.state.GetJob(req.JobID)
	}
	if job.Job.Config.Tty && req.Flags&host.AttachFlagStdin != 0 {
		resize := func() { h.docker.ResizeContainerTTY(job.ContainerID, req.Height, req.Width) }
		if job.Status == host.StatusRunning {
			resize()
		} else {
			var once sync.Once
			go func() {
				ch := make(chan host.Event)
				h.state.AddListener(req.JobID, ch)
				go func() {
					// There is a race that can result in the listener being
					// added after the container has started, so check the
					// status *after* subscribing.
					// This can deadlock if we try to get a state lock while an
					// event is being sent on the listen channel, so we do it
					// in the goroutine and wrap in a sync.Once.
					j := h.state.GetJob(req.JobID)
					if j.Status == host.StatusRunning {
						once.Do(resize)
					}
				}()
				defer h.state.RemoveListener(req.JobID, ch)
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

	success := make(chan struct{})
	failed := make(chan struct{})
	opts := docker.AttachToContainerOptions{
		Container:    job.ContainerID,
		InputStream:  conn,
		OutputStream: conn,
		Stdin:        req.Flags&host.AttachFlagStdin != 0,
		Stdout:       req.Flags&host.AttachFlagStdout != 0,
		Stderr:       req.Flags&host.AttachFlagStderr != 0,
		Logs:         req.Flags&host.AttachFlagLogs != 0,
		Stream:       req.Flags&host.AttachFlagStream != 0,
		Success:      success,
	}
	go func() {
		select {
		case <-success:
			conn.Write([]byte{host.AttachSuccess})
			close(success)
		case <-failed:
		}
		close(attachWait)
	}()
	if err := h.docker.AttachToContainer(opts); err != nil {
		select {
		case <-success:
		default:
			close(failed)
			conn.Write(append([]byte{host.AttachError}, err.Error()...))
		}
		g.Log(grohl.Data{"at": "docker", "status": "error", "err": err})
		return
	}
	g.Log(grohl.Data{"at": "finish"})
}
