package sampi

import (
	"encoding/json"
	"errors"
	"io/ioutil"
	"net/http"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/julienschmidt/httprouter"
	log "github.com/flynn/flynn/Godeps/_workspace/src/gopkg.in/inconshreveable/log15.v2"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/sse"
)

// Cluster
type Cluster struct {
	state  *State
	logger log.Logger
}

func NewCluster() *Cluster {
	return &Cluster{state: NewState(), logger: log.New("app", "sampi.cluster")}
}

// Scheduler Methods

func (s *Cluster) ListHosts() ([]host.Host, error) {
	s.logger.Debug("gathering host list", "fn", "ListHosts")
	hostMap := s.state.Get()
	hostSlice := make([]host.Host, 0, len(hostMap))
	for _, h := range hostMap {
		hostSlice = append(hostSlice, h)
	}
	return hostSlice, nil
}

func (s *Cluster) AddJobs(req map[string][]*host.Job) (map[string]host.Host, error) {
	l := s.logger.New("fn", "AddJobs")
	s.state.Begin()
	for hostID, jobs := range req {
		if err := s.state.AddJobs(hostID, jobs); err != nil {
			l.Error("error adding jobs to host", "host.id", hostID, "err", err)
			s.state.Rollback()
			return nil, err
		}
	}
	res := s.state.Commit()

	for hostID, jobs := range req {
		for _, job := range jobs {
			s.state.SendJob(hostID, job)
		}
	}

	return res, nil
}

// Host Service methods
func (s *Cluster) RemoveJobs(hostID string, jobIDs ...string) error {
	l := s.logger.New("fn", "RemoveJobs", "host.id", hostID)
	l.Debug("beginning to remove jobs")
	s.state.Begin()
	s.logger.Debug("now removing jobs from host")
	s.state.RemoveJobs(hostID, jobIDs...)
	s.state.Commit()
	return nil
}

func (s *Cluster) StreamHostEvents(ch chan host.HostEvent, done chan bool) error {
	l := s.logger.New("fn", "StreamHostEvents")
	l.Debug("adding host event listener", "at", "add_listener")
	s.state.AddListener(ch)
	go func() {
		<-done
		go func() {
			// drain to prevent deadlock while removing the listener
			for range ch {
			}
		}()
		l.Debug("removing host event listener", "at", "remove_listener")
		s.state.RemoveListener(ch)
		close(ch)
	}()
	return nil
}

type HTTPAPI struct {
	Cluster *Cluster
	logger  log.Logger
}

// HTTP Route Handles
func (c *HTTPAPI) ListHosts(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	l := c.logger.New("fn", "ListHosts")
	ret, err := c.Cluster.ListHosts()
	if err != nil {
		l.Error("error listing hosts", "err", err)
		httphelper.Error(w, err)
		return
	}
	l.Debug("writing http response")
	httphelper.JSON(w, 200, ret)
}

func (c *HTTPAPI) RegisterHost(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	l := c.logger.New("fn", "RegisterHost")
	h := &host.Host{}
	if err := json.NewDecoder(r.Body).Decode(&h); err != nil {
		l.Error("failed to decode host", "err", err)
		httphelper.Error(w, err)
		return
	}

	if h.ID == "" {
		l.Error("error registering host, the given host ID is blank")
		httphelper.Error(w, errors.New("sampi: host id must not be blank"))
		return
	}

	inCh := make(chan *host.Job)
	outCh := make(chan *host.Job)

	c.Cluster.state.Begin()
	if c.Cluster.state.HostExists(h.ID) {
		c.Cluster.state.Rollback()
		httphelper.Error(w, errors.New("sampi: host exists"))
		return
	}
	c.Cluster.state.AddHost(h, inCh)
	c.Cluster.state.Commit()
	go c.Cluster.state.sendEvent(h.ID, "add")

	go func() {
		ll := l.New("host.id", h.ID)
		ll.Debug("streaming jobs")
		for data := range inCh {
			ll.Debug("sending job event to registered host", "job.id", data.ID)
			outCh <- data
		}
	}()

	defer func() {
		l.Debug("host disconnected, unregistering", "at", "stream_close")
		c.Cluster.state.Begin()
		c.Cluster.state.RemoveHost(h.ID)
		c.Cluster.state.Commit()
		c.Cluster.state.sendEvent(h.ID, "remove")
	}()
	sse.ServeStream(w, outCh, l)
}

func (c *HTTPAPI) AddJobs(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	l := c.logger.New("fn", "AddJobs")
	data, err := ioutil.ReadAll(r.Body)
	if err != nil {
		l.Error("reading request body error", "err", err)
		httphelper.Error(w, err)
		return
	}

	var req map[string][]*host.Job
	if err := json.Unmarshal(data, &req); err != nil {
		l.Error("json unmarshal error", "err", err)
		httphelper.Error(w, err)
		return
	}
	res, err := c.Cluster.AddJobs(req)
	if err != nil {
		l.Error("error adding jobs to cluster", "err", err)
		httphelper.Error(w, err)
		return
	}
	l.Debug("writing http response")
	httphelper.JSON(w, 200, res)
}

func (c *HTTPAPI) RemoveJob(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	l := c.logger.New("fn", "RemoveJob")
	if err := c.Cluster.RemoveJobs(ps.ByName("host_id"), ps.ByName("job_id")); err != nil {
		l.Error("remove_jobs error", "err", err)
		httphelper.Error(w, err)
		return
	}
	w.WriteHeader(200)
}

func (c *HTTPAPI) StreamHostEvents(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	l := c.logger.New("fn", "StreamHostEvents")
	ch := make(chan host.HostEvent)
	done := make(chan bool)
	if err := c.Cluster.StreamHostEvents(ch, done); err != nil {
		l.Error("error requesting host event stream", "err", err)
		httphelper.Error(w, err)
		return
	}
	l.Debug("streaming host events")
	defer close(done)
	sse.ServeStream(w, ch, l)
	l.Debug("http stream closed")
}

func (c *HTTPAPI) RegisterRoutes(r *httprouter.Router) error {
	r.GET("/cluster/hosts", c.ListHosts)
	r.PUT("/cluster/hosts/:id", c.RegisterHost)
	r.POST("/cluster/jobs", c.AddJobs)
	r.DELETE("/cluster/hosts/:host_id/jobs/:job_id", c.RemoveJob)
	r.GET("/cluster/events", c.StreamHostEvents)
	return nil
}

func NewHTTPAPI(cluster *Cluster) *HTTPAPI {
	return &HTTPAPI{Cluster: cluster, logger: log.New("app", "sampi.http")}
}
