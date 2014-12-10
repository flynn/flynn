package main

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/julienschmidt/httprouter"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/technoweenie/grohl"
	"github.com/flynn/flynn/host/types"
)

type attachHandler struct {
	state   *State
	backend Backend
}

func (h *attachHandler) ServeHTTP(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
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
		g.Log(grohl.Data{"at": "wait"})
		<-attachWait
		job = h.state.GetJob(req.JobID)
	}
	w := bufio.NewWriter(conn)
	writeError := func(err string) {
		w.WriteByte(host.AttachError)
		binary.Write(w, binary.BigEndian, uint32(len(err)))
		w.WriteString(err)
		w.Flush()
	}
	if job.Status == host.StatusFailed {
		close(attachWait)
		writeError(*job.Error)
		return
	}

	writeMtx := &sync.Mutex{}
	writeMtx.Lock()

	attached := make(chan struct{})
	failed := make(chan struct{})
	opts := &AttachRequest{
		Job:      job,
		Logs:     req.Flags&host.AttachFlagLogs != 0,
		Stream:   req.Flags&host.AttachFlagStream != 0,
		Height:   req.Height,
		Width:    req.Width,
		Attached: attached,
	}
	var stdinW *io.PipeWriter
	if req.Flags&host.AttachFlagStdin != 0 {
		opts.Stdin, stdinW = io.Pipe()
	}
	if req.Flags&host.AttachFlagStdout != 0 {
		opts.Stdout = newFrameWriter(1, w, writeMtx)
	}
	if req.Flags&host.AttachFlagStderr != 0 {
		opts.Stderr = newFrameWriter(2, w, writeMtx)
	}

	go func() {
		defer func() {
			if stdinW != nil {
				stdinW.Close()
			}
		}()

		select {
		case <-attached:
			g.Log(grohl.Data{"at": "success"})
			conn.Write([]byte{host.AttachSuccess})
			writeMtx.Unlock()
			close(attached)
		case <-failed:
			g.Log(grohl.Data{"at": "failed"})
			writeMtx.Unlock()
			return
		}
		close(attachWait)
		r := bufio.NewReader(conn)
		var buf [4]byte

		for {
			frameType, err := r.ReadByte()
			if err != nil {
				// TODO: signal close to attach and close all connections
				return
			}
			switch frameType {
			case host.AttachData:
				stream, err := r.ReadByte()
				if err != nil || stream != 0 || stdinW == nil {
					return
				}
				if _, err := io.ReadFull(r, buf[:]); err != nil {
					return
				}
				length := int64(binary.BigEndian.Uint32(buf[:]))
				if length == 0 {
					stdinW.Close()
					stdinW = nil
					continue
				}
				if _, err := io.CopyN(stdinW, r, length); err != nil {
					return
				}
			case host.AttachSignal:
				if _, err := io.ReadFull(r, buf[:]); err != nil {
					return
				}
				signal := int(binary.BigEndian.Uint32(buf[:]))
				g.Log(grohl.Data{"at": "signal", "signal": signal})
				if err := h.backend.Signal(req.JobID, signal); err != nil {
					g.Log(grohl.Data{"at": "signal", "status": "error", "err": err})
					return
				}
			case host.AttachResize:
				if !job.Job.Config.TTY {
					return
				}
				if _, err := io.ReadFull(r, buf[:]); err != nil {
					return
				}
				height := binary.BigEndian.Uint16(buf[:])
				width := binary.BigEndian.Uint16(buf[2:])
				g.Log(grohl.Data{"at": "tty_resize", "height": height, "width": width})
				if err := h.backend.ResizeTTY(req.JobID, height, width); err != nil {
					g.Log(grohl.Data{"at": "tty_resize", "status": "error", "err": err})
					return
				}
			default:
				return
			}
		}
	}()

	g.Log(grohl.Data{"at": "attach"})
	if err := h.backend.Attach(opts); err != nil && err != io.EOF {
		if exit, ok := err.(ExitError); ok {
			writeMtx.Lock()
			w.WriteByte(host.AttachExit)
			binary.Write(w, binary.BigEndian, uint32(exit))
			w.Flush()
			writeMtx.Unlock()
		} else {
			close(failed)
			writeMtx.Lock()
			writeError(err.Error())
			writeMtx.Unlock()
			g.Log(grohl.Data{"at": "attach", "status": "error", "err": err.Error()})
		}
	} else {
		if opts.Stdout != nil {
			opts.Stdout.Close()
		}
		if opts.Stderr != nil {
			opts.Stderr.Close()
		}
	}
	g.Log(grohl.Data{"at": "finish"})
}

type ExitError int

func (e ExitError) Error() string {
	return fmt.Sprintf("exit status %d", e)
}

type frameWriter struct {
	mtx    *sync.Mutex
	buf    [6]byte
	w      *bufio.Writer
	closed bool
}

func newFrameWriter(stream byte, w *bufio.Writer, mtx *sync.Mutex) io.WriteCloser {
	f := &frameWriter{w: w, mtx: mtx}
	f.buf[0] = host.AttachData
	f.buf[1] = stream
	return f
}

func (w *frameWriter) Write(p []byte) (int, error) {
	w.mtx.Lock()
	defer w.mtx.Unlock()
	binary.BigEndian.PutUint32(w.buf[2:], uint32(len(p)))
	w.w.Write(w.buf[:])
	n, _ := w.w.Write(p)
	return n, w.w.Flush()
}

func (w *frameWriter) Close() error {
	w.mtx.Lock()
	defer w.mtx.Unlock()
	if w.closed {
		return nil
	}
	w.closed = true
	binary.BigEndian.PutUint32(w.buf[2:], 0)
	w.w.Write(w.buf[:])
	return w.w.Flush()
}
