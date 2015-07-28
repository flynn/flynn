package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	tuf "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-tuf/client"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/julienschmidt/httprouter"
	"github.com/flynn/flynn/host/downloader"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/host/volume/api"
	"github.com/flynn/flynn/host/volume/manager"
	"github.com/flynn/flynn/pinkerton"
	"github.com/flynn/flynn/pinkerton/layer"
	"github.com/flynn/flynn/pkg/cluster"
	"github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/shutdown"
	"github.com/flynn/flynn/pkg/sse"
	"github.com/flynn/flynn/pkg/version"
)

var (
	// ControlMsgResume is sent via a control socket from a parent daemon
	// to its child to request that the child start serving requests.
	ControlMsgResume = []byte{0}

	// ControlMsgOK is sent via a control socket from a child daemon to
	// its parent to indicate it has received a resume request and is now
	// serving requests.
	ControlMsgOK = []byte{1}
)

type Host struct {
	state   *State
	backend Backend
	vman    *volumemanager.Manager
	id      string
	url     string

	statusMtx sync.RWMutex
	status    *host.HostStatus

	connectDiscoverd func(string) error
	discoverdOnce    sync.Once
	networkOnce      sync.Once

	listener net.Listener
}

var ErrNotFound = errors.New("host: unknown job")

func (h *Host) StopJob(id string) error {
	if err := h.state.Acquire(); err != nil {
		return err
	}
	defer h.state.Release()

	job := h.state.GetJob(id)
	if job == nil {
		return ErrNotFound
	}
	switch job.Status {
	case host.StatusStarting:
		h.state.SetForceStop(id)
		return nil
	case host.StatusRunning:
		return h.backend.Stop(id)
	default:
		return errors.New("host: job is already stopped")
	}
}

func (h *Host) SignalJob(id string, sig int) error {
	job := h.state.GetJob(id)
	if job == nil {
		return ErrNotFound
	}
	return h.backend.Signal(id, sig)
}

func (h *Host) streamEvents(id string, w http.ResponseWriter) error {
	ch := h.state.AddListener(id)
	defer h.state.RemoveListener(id, ch)
	sse.ServeStream(w, ch, nil)
	return nil
}

type jobAPI struct {
	host *Host
}

func (h *jobAPI) ListJobs(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	if strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
		if err := h.host.streamEvents("all", w); err != nil {
			httphelper.Error(w, err)
		}
		return
	}
	res := h.host.state.Get()

	httphelper.JSON(w, 200, res)
}

func (h *jobAPI) GetJob(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	id := ps.ByName("id")

	if strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
		if err := h.host.streamEvents(id, w); err != nil {
			httphelper.Error(w, err)
		}
		return
	}

	job := h.host.state.GetJob(id)
	if job == nil {
		httphelper.ObjectNotFoundError(w, ErrNotFound.Error())
		return
	}
	httphelper.JSON(w, 200, job)
}

func (h *jobAPI) StopJob(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	id := ps.ByName("id")
	if err := h.host.StopJob(id); err != nil {
		httphelper.Error(w, err)
		return
	}
	w.WriteHeader(200)
}

func (h *jobAPI) SignalJob(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	sig := ps.ByName("signal")
	if sig == "" {
		httphelper.ValidationError(w, "sig", "must not be empty")
		return
	}
	sigInt, err := strconv.Atoi(sig)
	if err != nil {
		httphelper.ValidationError(w, "sig", "must be an integer")
		return
	}
	id := ps.ByName("id")
	if err := h.host.SignalJob(id, sigInt); err != nil {
		httphelper.Error(w, err)
		return
	}
	w.WriteHeader(200)
}

func (h *jobAPI) PullImages(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	tufDB, err := extractTufDB(r)
	if err != nil {
		httphelper.Error(w, err)
		return
	}
	defer os.Remove(tufDB)

	info := make(chan layer.PullInfo)
	stream := sse.NewStream(w, info, nil)
	go stream.Serve()

	if err := pinkerton.PullImages(
		tufDB,
		r.URL.Query().Get("repository"),
		r.URL.Query().Get("driver"),
		r.URL.Query().Get("root"),
		info,
	); err != nil {
		stream.CloseWithError(err)
		return
	}

	stream.Wait()
}

func (h *jobAPI) PullBinariesAndConfig(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	tufDB, err := extractTufDB(r)
	if err != nil {
		httphelper.Error(w, err)
		return
	}
	defer os.Remove(tufDB)

	query := r.URL.Query()
	local, err := tuf.FileLocalStore(tufDB)
	if err != nil {
		httphelper.Error(w, err)
		return
	}
	opts := &tuf.HTTPRemoteOptions{
		UserAgent: fmt.Sprintf("flynn-host/%s %s-%s pull", version.String(), runtime.GOOS, runtime.GOARCH),
	}
	remote, err := tuf.HTTPRemoteStore(query.Get("repository"), opts)
	if err != nil {
		httphelper.Error(w, err)
		return
	}
	client := tuf.NewClient(local, remote)

	paths, err := downloader.DownloadBinaries(client, query.Get("bin-dir"))
	if err != nil {
		httphelper.Error(w, err)
		return
	}

	configs, err := downloader.DownloadConfig(client, query.Get("config-dir"))
	if err != nil {
		httphelper.Error(w, err)
		return
	}
	for k, v := range configs {
		paths[k] = v
	}

	httphelper.JSON(w, 200, paths)
}

func (h *jobAPI) AddJob(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	if shutdown.IsActive() {
		httphelper.JSON(w, 500, struct{}{})
		return
	}

	job := &host.Job{}
	if err := httphelper.DecodeJSON(r, job); err != nil {
		httphelper.Error(w, err)
		return
	}
	// TODO(titanous): validate UUID
	job.ID = ps.ByName("id")

	if err := h.host.state.Acquire(); err != nil {
		httphelper.Error(w, err)
		return
	}

	go func() {
		// TODO(titanous): ratelimit this goroutine?
		err := h.host.backend.Run(job, nil)
		h.host.state.Release()
		if err != nil {
			h.host.state.SetStatusFailed(job.ID, err)
		}
	}()

	// TODO(titanous): return 201 Accepted
	httphelper.JSON(w, 200, struct{}{})
}

func (h *jobAPI) ConfigureDiscoverd(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var config host.DiscoverdConfig
	if err := httphelper.DecodeJSON(r, &config); err != nil {
		httphelper.Error(w, err)
		return
	}

	h.host.statusMtx.Lock()
	h.host.status.Discoverd = &config
	h.host.statusMtx.Unlock()

	if config.URL != "" && config.DNS != "" {
		go h.host.discoverdOnce.Do(func() {
			if err := h.host.connectDiscoverd(config.URL); err != nil {
				shutdown.Fatal(err)
			}
		})
	}
}

func (h *jobAPI) ConfigureNetworking(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	config := &host.NetworkConfig{}
	if err := httphelper.DecodeJSON(r, config); err != nil {
		shutdown.Fatal(err)
	}

	// configure the network before returning a response in case the
	// network coordinator requires the bridge to be created (e.g.
	// when using flannel with the "alloc" backend)
	h.host.networkOnce.Do(func() {
		if err := h.host.backend.ConfigureNetworking(config); err != nil {
			shutdown.Fatal(err)
		}

		h.host.statusMtx.Lock()
		h.host.status.Network = config
		h.host.statusMtx.Unlock()
	})
}

func (h *jobAPI) GetStatus(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	h.host.statusMtx.RLock()
	defer h.host.statusMtx.RUnlock()
	httphelper.JSON(w, 200, &h.host.status)
}

func checkPort(port host.Port) bool {
	l, err := net.Listen(port.Proto, fmt.Sprintf(":%d", port.Port))
	if err != nil {
		return false
	}
	l.Close()
	return true
}

func (h *jobAPI) ResourceCheck(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var req host.ResourceCheck
	if err := httphelper.DecodeJSON(r, &req); err != nil {
		httphelper.Error(w, err)
		return
	}
	var conflicts []host.Port
	for _, p := range req.Ports {
		if p.Proto == "" {
			p.Proto = "tcp"
		}
		if !checkPort(p) {
			conflicts = append(conflicts, p)
		}
	}
	if len(conflicts) > 0 {
		resp := host.ResourceCheck{Ports: conflicts}
		detail, err := json.Marshal(resp)
		if err != nil {
			httphelper.Error(w, err)
			return
		}
		httphelper.JSON(w, 409, &httphelper.JSONError{
			Code:    httphelper.ConflictErrorCode,
			Message: "Conflicting resources found",
			Detail:  detail,
		})
		return
	}
	httphelper.JSON(w, 200, struct{}{})
}

func (h *jobAPI) Update(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	var cmd host.Command
	if err := httphelper.DecodeJSON(req, &cmd); err != nil {
		httphelper.Error(w, err)
		return
	}

	err := h.host.Update(&cmd)
	if err != nil {
		httphelper.Error(w, err)
		return
	}

	// send an ok response and then shutdown after 1s to give the response
	// chance to reach the client.
	httphelper.JSON(w, http.StatusOK, cmd)
	time.AfterFunc(time.Second, func() { os.Exit(0) })
}

func extractTufDB(r *http.Request) (string, error) {
	defer r.Body.Close()
	tmp, err := ioutil.TempFile("", "tuf-db")
	if err != nil {
		return "", err
	}
	defer tmp.Close()
	if _, err := io.Copy(tmp, r.Body); err != nil {
		return "", err
	}
	return tmp.Name(), nil
}

func (h *jobAPI) RegisterRoutes(r *httprouter.Router) error {
	r.GET("/host/jobs", h.ListJobs)
	r.GET("/host/jobs/:id", h.GetJob)
	r.PUT("/host/jobs/:id", h.AddJob)
	r.DELETE("/host/jobs/:id", h.StopJob)
	r.PUT("/host/jobs/:id/signal/:signal", h.SignalJob)
	r.POST("/host/pull/images", h.PullImages)
	r.POST("/host/pull/binaries", h.PullBinariesAndConfig)
	r.POST("/host/discoverd", h.ConfigureDiscoverd)
	r.POST("/host/network", h.ConfigureNetworking)
	r.GET("/host/status", h.GetStatus)
	r.POST("/host/resource-check", h.ResourceCheck)
	r.POST("/host/update", h.Update)
	return nil
}

func (h *Host) ServeHTTP() {
	r := httprouter.New()

	r.POST("/attach", (&attachHandler{state: h.state, backend: h.backend}).ServeHTTP)

	jobAPI := &jobAPI{host: h}
	jobAPI.RegisterRoutes(r)

	volAPI := volumeapi.NewHTTPAPI(cluster.NewClient(), h.vman)
	volAPI.RegisterRoutes(r)

	go http.Serve(h.listener, httphelper.ContextInjector("host", httphelper.NewRequestLogger(r)))
}

func (h *Host) OpenDBs() error {
	if err := h.state.OpenDB(); err != nil {
		return err
	}
	return h.vman.OpenDB()
}

func (h *Host) CloseDBs() error {
	if err := h.state.CloseDB(); err != nil {
		return err
	}
	return h.vman.CloseDB()
}

func (h *Host) Close() error {
	if h.listener != nil {
		return h.listener.Close()
	}
	return nil
}

func newHTTPListener(addr string) (net.Listener, error) {
	fdEnv := os.Getenv("FLYNN_HTTP_FD")
	if fdEnv == "" {
		return net.Listen("tcp", addr)
	}
	fd, err := strconv.Atoi(fdEnv)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), "http")
	defer file.Close()
	return net.FileListener(file)
}

// Update performs a zero-downtime update of the flynn-host daemon, replacing
// the current daemon with an instance of the given command.
//
// The HTTP API listener is passed from the parent to the child, but due to the
// state DBs being process exclusive and requiring initialisation, further
// syncronisation is required to manage opening and closing them, which is done
// using a control socket.
//
// An outline of the process:
//
// * parent receives a request to exec a new daemon
//
// * parent creates a control socket pair (via socketpair(2))
//
// * parent starts a child process, passing the API listener as FD 3, and a
//   control socket as FD 4
//
// * parent closes its API listener FD and state DBs
//
// * parent signals the child to resume by sending "resume" message to control
//   socket
//
// * child receives resume request, opens state DBs and starts serving API
//   requests
//
// * child signals parent it is now serving requests by sending "ok" message to
//   control socket
//
// * parent sends response to client and shuts down seconds later
//
func (h *Host) Update(cmd *host.Command) error {
	// dup the listener so we can close the current listener but still be
	// able continue serving requests if the child exits by using the dup'd
	// listener.
	file, err := h.listener.(*net.TCPListener).File()
	if err != nil {
		return err
	}
	defer file.Close()

	// exec a child, passing the listener and control socket as extra files
	child, err := h.exec(cmd, file)
	if err != nil {
		return err
	}
	defer child.CloseSock()

	// close our listener and state DBs
	h.listener.Close()
	if err := h.CloseDBs(); err != nil {
		return err
	}

	// signal the child to resume
	if resumeErr := child.Resume(); resumeErr != nil {
		// the child failed to resume, kill it and resume ourselves
		child.Kill()
		l, err := net.FileListener(file)
		if err != nil {
			return err
		}
		h.listener = l
		if err := h.OpenDBs(); err != nil {
			return err
		}
		h.ServeHTTP()
		return resumeErr
	}

	return nil
}

func (h *Host) exec(cmd *host.Command, listener *os.File) (*Child, error) {
	// create a control socket for communicating with the child
	sockPair, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
	if err != nil {
		return nil, err
	}

	dir, err := os.Getwd()
	if err != nil {
		return nil, err
	}

	h.statusMtx.RLock()
	status, err := json.Marshal(h.status)
	h.statusMtx.RUnlock()
	if err != nil {
		return nil, err
	}

	c := exec.Command(cmd.Path, cmd.Args...)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	c.Dir = dir
	c.ExtraFiles = []*os.File{
		listener,
		os.NewFile(uintptr(sockPair[1]), "child"),
	}
	setEnv(c, map[string]string{
		"FLYNN_HTTP_FD":     "3",
		"FLYNN_CONTROL_FD":  "4",
		"FLYNN_HOST_STATUS": string(status),
	})
	if err := c.Start(); err != nil {
		return nil, err
	}
	cmd.PID = c.Process.Pid
	return &Child{c, sockPair[0]}, syscall.Close(sockPair[1])
}

// setEnv sets the given environment variables for the command, ensuring they
// are only set once.
func setEnv(cmd *exec.Cmd, envs map[string]string) {
	env := os.Environ()
	cmd.Env = make([]string, 0, len(env)+len(envs))
outer:
	for _, e := range env {
		for k := range envs {
			if strings.HasPrefix(e, k+"=") {
				continue outer
			}
		}
		cmd.Env = append(cmd.Env, e)
	}
	for k, v := range envs {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
}

type Child struct {
	cmd  *exec.Cmd
	sock int
}

// Resume writes ControlMsgResume to the control socket and waits for a
// ControlMsgOK response
func (c *Child) Resume() error {
	if _, err := syscall.Write(c.sock, ControlMsgResume); err != nil {
		return err
	}
	msg := make([]byte, len(ControlMsgOK))
	if _, err := syscall.Read(c.sock, msg); err != nil {
		return err
	}
	if !bytes.Equal(msg, ControlMsgOK) {
		return fmt.Errorf("unexpected resume message from child: %s", msg)
	}
	return nil
}

func (c *Child) CloseSock() error {
	return syscall.Close(c.sock)
}

func (c *Child) Kill() error {
	if err := c.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		return c.cmd.Process.Kill()
	}
	done := make(chan struct{})
	go func() {
		c.cmd.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-time.After(10 * time.Second):
		return c.cmd.Process.Kill()
	}
}
