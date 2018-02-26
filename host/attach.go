package main

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"

	"github.com/flynn/flynn/host/types"
	"github.com/julienschmidt/httprouter"
	"github.com/inconshreveable/log15"
)

type attachHandler struct {
	state   *State
	backend Backend

	// attached is a map of job IDs which are currently attached and is
	// used to prevent multiple clients attaching to interactive jobs (i.e.
	// ones which have DisableLog set)
	attached    map[string]struct{}
	attachedMtx sync.Mutex

	logger log15.Logger
}

func newAttachHandler(state *State, backend Backend, logger log15.Logger) *attachHandler {
	return &attachHandler{
		state:    state,
		backend:  backend,
		logger:   logger,
		attached: make(map[string]struct{}),
	}
}

func (h *attachHandler) ServeHTTP(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	var attachReq host.AttachReq
	if err := json.NewDecoder(req.Body).Decode(&attachReq); err != nil {
		http.Error(w, "invalid JSON", 400)
		return
	}
	w.Header().Set("Connection", "upgrade")
	w.Header().Set("Upgrade", "flynn-attach/0")
	w.WriteHeader(http.StatusSwitchingProtocols)

	conn, _, err := w.(http.Hijacker).Hijack()
	if err != nil {
		return
	}
	h.attach(&attachReq, conn)
}

func (h *attachHandler) attach(req *host.AttachReq, conn io.ReadWriteCloser) {
	defer conn.Close()
	log := h.logger.New("fn", "attach", "job.id", req.JobID)
	log.Info("starting")
	attachWait := make(chan struct{})
	job := h.state.AddAttacher(req.JobID, attachWait)
	if job == nil {
		defer h.state.RemoveAttacher(req.JobID, attachWait)
		if _, err := conn.Write([]byte{host.AttachWaiting}); err != nil {
			return
		}
		// TODO: add timeout
		log.Info("waiting for attach")
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

	// if the job has DisableLog set and is already attached, return an
	// error, otherwise mark it as attached
	h.attachedMtx.Lock()
	if _, ok := h.attached[job.Job.ID]; ok && job.Job.Config.DisableLog {
		h.attachedMtx.Unlock()
		writeError(host.ErrAttached.Error())
		return
	}
	h.attached[job.Job.ID] = struct{}{}
	h.attachedMtx.Unlock()
	defer func() {
		h.attachedMtx.Lock()
		delete(h.attached, job.Job.ID)
		h.attachedMtx.Unlock()
	}()

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
	if req.Flags&host.AttachFlagInitLog != 0 {
		opts.InitLog = newFrameWriter(3, w, writeMtx)
	}

	go func() {
		defer func() {
			if stdinW != nil {
				stdinW.Close()
			}
		}()

		select {
		case <-attached:
			log.Info("successfully attached")
			conn.Write([]byte{host.AttachSuccess})
			writeMtx.Unlock()
			close(attached)
		case <-failed:
			log.Error("failed to attach to job")
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
				log.Info("signaling", "signal", signal)
				if err := h.backend.Signal(req.JobID, signal); err != nil {
					log.Error("error signalling job", "err", err)
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
				log.Info("resizing tty", "height", height, "width", width)
				if err := h.backend.ResizeTTY(req.JobID, height, width); err != nil {
					log.Error("error resizing tty", "err", err)
					return
				}
			default:
				return
			}
		}
	}()

	log.Info("attaching")
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
			log.Error("attach error", "err", err)
		}
	} else {
		if opts.Stdout != nil {
			opts.Stdout.Close()
		}
		if opts.Stderr != nil {
			opts.Stderr.Close()
		}
	}
	log.Info("finished")
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
