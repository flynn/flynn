package bootstrap

import (
	"fmt"

	"github.com/flynn/flynn-host/types"
	"github.com/flynn/go-flynn/cluster"
)

type RunJobAction struct {
	ID       string    `json:"id"`
	Job      *host.Job `json:"job"`
	HostTags []string  `json:"host_tags,omitempty"`
}

func init() {
	Register("run-job", &RunJobAction{})
}

type RunJobState struct {
	*Job
}

func (a *RunJobAction) Run(s *State) (err error) {
	js := &RunJobState{}
	s.StepData[a.ID] = js

	js.Job, err = startJob(s, a.HostTags, a.Job)
	return
}

func startJob(s *State, hostTags []string, job *host.Job) (*Job, error) {
	cc, err := s.ClusterClient()
	if err != nil {
		return nil, err
	}
	h, err := randomHost(cc)
	if err != nil {
		return nil, err
	}

	// TODO: filter by tags

	job.ID = cluster.RandomJobID("")
	data := &Job{HostID: h.ID, JobID: job.ID}

	hc, err := cc.ConnectHost(h.ID)
	if err != nil {
		return nil, err
	}
	defer hc.Close()

	jobStatus := make(chan error)
	events := make(chan *host.Event)
	stream := hc.StreamEvents(data.JobID, events)
	go func() {
		defer stream.Close()
		for e := range events {
			switch e.Event {
			case "start", "stop":
				jobStatus <- nil
				return
			case "error":
				job, err := hc.GetJob(data.JobID)
				if err != nil {
					jobStatus <- err
					return
				}
				if job.Error == nil {
					jobStatus <- fmt.Errorf("bootstrap: unknown error from host")
					return
				}
				jobStatus <- fmt.Errorf("bootstrap: host error while launching job: %q", *job.Error)
				return
			default:
			}
		}
		jobStatus <- fmt.Errorf("bootstrap: host job stream disconnected unexpectedly: %q", stream.Err())
	}()

	_, err = cc.AddJobs(&host.AddJobsReq{HostJobs: map[string][]*host.Job{h.ID: {job}}})
	if err != nil {
		return nil, err
	}

	return data, <-jobStatus
}

func randomHost(cc *cluster.Client) (*host.Host, error) {
	hosts, err := cc.ListHosts()
	if err != nil {
		return nil, err
	}

	for _, host := range hosts {
		return &host, nil
	}
	return nil, cluster.ErrNoServers
}

func (a *RunJobAction) Cleanup(s *State) error {
	data, ok := s.StepData[a.ID].(*RunJobState)
	if !ok {
		return nil
	}

	if data.HostID != "" && data.JobID != "" {
		cc, err := s.ClusterClient()
		if err != nil {
			return err
		}
		h, err := cc.ConnectHost(data.HostID)
		if err != nil {
			return err
		}
		defer h.Close()
		if err := h.StopJob(data.JobID); err != nil {
			return err
		}
	}

	return nil
}
