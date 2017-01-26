package main

import (
	"fmt"
	"sync"
	"time"

	"github.com/flynn/flynn/controller/testutils"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/controller/utils"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/host/volume"
	"github.com/flynn/flynn/pkg/stream"
	"gopkg.in/inconshreveable/log15.v2"
)

type Host struct {
	ID       string            `json:"id"`
	Tags     map[string]string `json:"tags"`
	Healthy  bool              `json:"healthy"`
	Checks   int               `json:"checks"`
	Shutdown bool              `json:"shutdown"`

	client   utils.HostClient
	stop     chan struct{}
	stopOnce sync.Once
	jobsDone chan struct{}
	volsDone chan struct{}
	logger   log15.Logger
}

func NewHost(h utils.HostClient, l log15.Logger) *Host {
	return &Host{
		ID:       h.ID(),
		Tags:     h.Tags(),
		Healthy:  true,
		client:   h,
		stop:     make(chan struct{}),
		jobsDone: make(chan struct{}),
		volsDone: make(chan struct{}),
		logger:   l,
	}
}

func (h *Host) TagsEqual(tags map[string]string) bool {
	if len(h.Tags) != len(tags) {
		return false
	}
	for k, v := range h.Tags {
		if w, ok := tags[k]; !ok || w != v {
			return false
		}
	}
	return true
}

func (h *Host) StreamVolumeEventsTo(ch chan *VolumeEvent) (map[string]*Volume, error) {
	log := h.logger.New("fn", "StreamVolumeEventsTo", "host.id", h.ID)
	var events chan *volume.Event
	var stream stream.Stream
	connect := func() (err error) {
		log.Info("connecting volume event stream")
		events = make(chan *volume.Event)
		stream, err = h.client.StreamVolumes(events)
		if err != nil {
			log.Error("error connecting volume event stream", "err", err)
		}
		return
	}
	if err := connect(); err != nil {
		return nil, err
	}

	log.Info("getting volumes")
	volumes, err := h.client.ListVolumes()
	if err != nil {
		log.Error("error getting volumes", "err", err)
		return nil, err
	}
	log.Info(fmt.Sprintf("got %d volumes for host %s", len(volumes), h.ID))
	vols := make(map[string]*Volume)
	for _, vol := range volumes {
		vols[vol.ID] = &Volume{Info: vol, HostID: h.ID}
	}

	go func() {
		defer stream.Close()
		defer close(h.volsDone)
		for {
		eventLoop:
			for {
				select {
				case event, ok := <-events:
					if !ok {
						break eventLoop
					}
					ch <- &VolumeEvent{
						Type:   event.Type,
						Volume: &Volume{Info: event.Volume, HostID: h.ID},
					}
				case <-h.stop:
					return
				}
			}

			log.Warn("volume event stream disconnected", "err", stream.Err())
			// keep trying to reconnect, unless we are told to stop
		retryLoop:
			for {
				select {
				case <-h.stop:
					return
				default:
				}

				if err := connect(); err == nil {
					break retryLoop
				}
				time.Sleep(100 * time.Millisecond)
			}
		}
	}()
	return vols, nil
}

// StreamJobEventsTo streams all job events from the host to the given channel
// in a goroutine, returning the current list of active jobs.
func (h *Host) StreamJobEventsTo(ch chan *host.Event) (map[string]host.ActiveJob, error) {
	log := h.logger.New("fn", "StreamJobEventsTo", "host.id", h.ID)
	var events chan *host.Event
	var stream stream.Stream
	connect := func() (err error) {
		log.Info("connecting job event stream")
		events = make(chan *host.Event)
		stream, err = h.client.StreamEvents("all", events)
		if err != nil {
			log.Error("error connecting job event stream", "err", err)
		}
		return
	}
	if err := connect(); err != nil {
		return nil, err
	}

	log.Info("getting active jobs")
	jobs, err := h.client.ListJobs()
	if err != nil {
		log.Error("error getting active jobs", "err", err)
		return nil, err
	}
	log.Info(fmt.Sprintf("got %d active job(s) for host %s", len(jobs), h.ID))

	go func() {
		defer stream.Close()
		defer close(h.jobsDone)
		for {
		eventLoop:
			for {
				select {
				case event, ok := <-events:
					if !ok {
						break eventLoop
					}
					ch <- event

					// if the host is a FakeHostClient with TestEventHook
					// set, send on the channel to synchronize with tests
					if h, ok := h.client.(*testutils.FakeHostClient); ok && h.TestEventHook != nil {
						h.TestEventHook <- struct{}{}
					}
				case <-h.stop:
					return
				}
			}

			log.Warn("job event stream disconnected", "err", stream.Err())
			// keep trying to reconnect, unless we are told to stop
		retryLoop:
			for {
				select {
				case <-h.stop:
					return
				default:
				}

				if err := connect(); err == nil {
					break retryLoop
				}
				time.Sleep(100 * time.Millisecond)
			}
		}
	}()
	return jobs, nil
}

func (h *Host) GetSinks() ([]*ct.Sink, error) {
	return h.client.GetSinks()
}

func (h *Host) AddSink(sink *ct.Sink) error {
	return h.client.AddSink(sink)
}

func (h *Host) RemoveSink(id string) error {
	return h.client.RemoveSink(id)
}

func (h *Host) Close() {
	h.stopOnce.Do(func() {
		close(h.stop)
		<-h.jobsDone
		<-h.volsDone
	})
}
