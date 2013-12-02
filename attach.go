package main

import (
	"encoding/json"
	"io"
	"net/http"
	"sync"

	"github.com/flynn/lorne/types"
	"github.com/technoweenie/grohl"
	"github.com/titanous/go-dockerclient"
)

type attachHandler struct {
	state  *State
	docker dockerAttachClient
}

func (h *attachHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	var attachReq lorne.AttachReq
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

func (h *attachHandler) attach(req *lorne.AttachReq, conn io.ReadWriteCloser) {
	defer conn.Close()

	g := grohl.NewContext(grohl.Data{"fn": "attach", "job.id": req.JobID})
	g.Log(grohl.Data{"at": "start"})
	attachWait := make(chan struct{})
	job := h.state.AddAttacher(req.JobID, attachWait)
	if job == nil {
		defer h.state.RemoveAttacher(req.JobID, attachWait)
		if _, err := conn.Write([]byte{lorne.AttachWaiting}); err != nil {
			return
		}
		// TODO: add timeout
		<-attachWait
		job = h.state.GetJob(req.JobID)
	}
	if job.Job.Config.Tty && req.Flags&lorne.AttachFlagStdin != 0 {
		resize := func() { h.docker.ResizeContainerTTY(job.ContainerID, req.Height, req.Width) }
		if job.Status == lorne.StatusRunning {
			resize()
		} else {
			var once sync.Once
			go func() {
				ch := make(chan lorne.Event)
				h.state.AddListener(req.JobID, ch)
				go func() {
					// There is a race that can result in the listener being
					// added after the container has started, so check the
					// status *after* subscribing.
					// This can deadlock if we try to get a state lock while an
					// event is being sent on the listen channel, so we do it
					// in the goroutine and wrap in a sync.Once.
					j := h.state.GetJob(req.JobID)
					if j.Status == lorne.StatusRunning {
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
		Stdin:        req.Flags&lorne.AttachFlagStdin != 0,
		Stdout:       req.Flags&lorne.AttachFlagStdout != 0,
		Stderr:       req.Flags&lorne.AttachFlagStderr != 0,
		Logs:         req.Flags&lorne.AttachFlagLogs != 0,
		Stream:       req.Flags&lorne.AttachFlagStream != 0,
		Success:      success,
	}
	go func() {
		select {
		case <-success:
			conn.Write([]byte{lorne.AttachSuccess})
			close(success)
		case <-failed:
		}
		close(attachWait)
	}()
	if err := h.docker.AttachToContainer(opts); err != nil {
		close(failed)
		conn.Write(append([]byte{lorne.AttachError}, err.Error()...))
		return
	}
	g.Log(grohl.Data{"at": "finish"})
}
