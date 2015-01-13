package sampi

import (
	"encoding/json"
	"errors"
	"io/ioutil"
	"net/http"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/julienschmidt/httprouter"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/shutdown"
	"github.com/flynn/flynn/pkg/sse"
)

// Cluster
type Cluster struct {
	state *State
}

func NewCluster(state *State) *Cluster {
	return &Cluster{state}
}

// Scheduler Methods

func (s *Cluster) ListHosts() ([]host.Host, error) {
	hostMap := s.state.Get()
	hostSlice := make([]host.Host, 0, len(hostMap))
	for _, h := range hostMap {
		hostSlice = append(hostSlice, h)
	}
	return hostSlice, nil
}

func (s *Cluster) AddJobs(req map[string][]*host.Job) (map[string]host.Host, error) {
	s.state.Begin()
	for host, jobs := range req {
		if err := s.state.AddJobs(host, jobs); err != nil {
			s.state.Rollback()
			return nil, err
		}
	}
	res := s.state.Commit()

	for host, jobs := range req {
		for _, job := range jobs {
			s.state.SendJob(host, job)
		}
	}

	return res, nil
}

// Host Service methods

func (s *Cluster) RemoveJobs(hostID string, jobIDs ...string) error {
	s.state.Begin()
	s.state.RemoveJobs(hostID, jobIDs...)
	s.state.Commit()
	return nil
}

func (s *Cluster) StreamHostEvents(ch chan host.HostEvent, done chan bool) error {
	s.state.AddListener(ch)
	go func() {
		<-done
		go func() {
			// drain to prevent deadlock while removing the listener
			for range ch {
			}
		}()
		s.state.RemoveListener(ch)
		close(ch)
	}()
	return nil
}

type httpAPI struct {
	cluster *Cluster
}

/* This will setup a SSEWriter + JSONEncoder and Write out the required Headers
 * to the provided HTTP, the Encoder returned is used to write to the SSE
 * Stream + JSON from data/objects/structs.
 */
func startJSONEventStreaming(w http.ResponseWriter) *json.Encoder {
	wr := sse.NewSSEWriter(w)
	enc := json.NewEncoder(httphelper.FlushWriter{Writer: wr, Enabled: true})
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.WriteHeader(200)
	wr.Flush()
	return enc
}

// HTTP Route Handles
func (c *httpAPI) ListHosts(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	ret, err := c.cluster.ListHosts()
	if err != nil {
		httphelper.Error(w, err)
		return
	}
	httphelper.JSON(w, 200, ret)
}

func (c *httpAPI) RegisterHost(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	h := &host.Host{}
	if err := json.NewDecoder(r.Body).Decode(&h); err != nil {
		httphelper.Error(w, err)
		return
	}

	if h.ID == "" {
		httphelper.Error(w, errors.New("sampi: host id must not be blank"))
		return
	}

	ch := make(chan *host.Job)

	c.cluster.state.Begin()
	if c.cluster.state.HostExists(h.ID) {
		c.cluster.state.Rollback()
		httphelper.Error(w, errors.New("sampi: host exists"))
		return
	}
	c.cluster.state.AddHost(h, ch)
	c.cluster.state.Commit()
	go c.cluster.state.sendEvent(h.ID, "add")

	// "defer" cleanups in a goroutine that waits until the http stream is closed.
	go func() {
		<-w.(http.CloseNotifier).CloseNotify()
		c.cluster.state.Begin()
		c.cluster.state.RemoveHost(h.ID)
		c.cluster.state.Commit()
		c.cluster.state.sendEvent(h.ID, "remove")
	}()

	enc := startJSONEventStreaming(w)
	for data := range ch {
		enc.Encode(data)
	}
}

func (c *httpAPI) AddJobs(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	data, err := ioutil.ReadAll(r.Body)
	if err != nil {
		httphelper.Error(w, err)
		return
	}

	var req map[string][]*host.Job
	if err := json.Unmarshal(data, &req); err != nil {
		httphelper.Error(w, err)
		return
	}
	res, err := c.cluster.AddJobs(req)
	if err != nil {
		httphelper.Error(w, err)
		return
	}
	httphelper.JSON(w, 200, res)
}

func (c *httpAPI) RemoveJob(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	if err := c.cluster.RemoveJobs(ps.ByName("host_id"), ps.ByName("job_id")); err != nil {
		httphelper.Error(w, err)
		return
	}
	w.WriteHeader(200)
}

func (c *httpAPI) StreamHostEvents(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	ch := make(chan host.HostEvent)
	done := make(chan bool)
	if err := c.cluster.StreamHostEvents(ch, done); err != nil {
		httphelper.Error(w, err)
		return
	}
	go func() {
		<-w.(http.CloseNotifier).CloseNotify()
		close(done)
	}()
	enc := startJSONEventStreaming(w)
	for data := range ch {
		enc.Encode(data)
	}
}

func (c *httpAPI) RegisterRoutes(r *httprouter.Router, sh *shutdown.Handler) error {
	r.GET("/cluster/hosts", c.ListHosts)
	r.PUT("/cluster/hosts/:id", c.RegisterHost)
	r.POST("/cluster/jobs", c.AddJobs)
	r.DELETE("/cluster/hosts/:host_id/jobs/:job_id", c.RemoveJob)
	r.GET("/cluster/events", c.StreamHostEvents)
	return nil
}

func NewHTTPAPI(c *Cluster) *httpAPI {
	return &httpAPI{c}
}
