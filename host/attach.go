package main

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/flynn/flynn-host/types"
	"github.com/flynn/go-dockerclient"
	"github.com/technoweenie/grohl"
)

type attachHandler struct {
	state   *State
	backend Backend
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

	success := make(chan struct{})
	failed := make(chan struct{})
	opts := &AttachRequest{
		Job:        job,
		Logs:       req.Flags&host.AttachFlagLogs != 0,
		Stream:     req.Flags&host.AttachFlagStream != 0,
		Height:     req.Height,
		Width:      req.Width,
		Attached:   success,
		ReadWriter: conn,
		Streams:    make([]string, 0, 3),
	}
	if req.Flags&host.AttachFlagStdin != 0 {
		opts.Streams = append(opts.Streams, "stdin")
	}
	if req.Flags&host.AttachFlagStdout != 0 {
		opts.Streams = append(opts.Streams, "stdout")
	}
	if req.Flags&host.AttachFlagStderr != 0 {
		opts.Streams = append(opts.Streams, "stderr")
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
	if err := h.backend.Attach(opts); err != nil {
		select {
		case <-success:
		default:
			close(failed)
			conn.Write(append([]byte{host.AttachError}, err.Error()...))
		}
		g.Log(grohl.Data{"status": "error", "err": err})
		return
	}
	g.Log(grohl.Data{"at": "finish"})
}
