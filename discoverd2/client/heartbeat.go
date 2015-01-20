package discoverd

import (
	"fmt"
	"log"
	"sync"
	"time"
)

type Heartbeater interface {
	SetMeta(map[string]string) error
	Close() error
}

func (c *Client) Register(service, addr string) (Heartbeater, error) {
	return c.RegisterInstance(service, &Instance{Addr: addr, Proto: "tcp"})
}

func (c *Client) RegisterInstance(service string, inst *Instance) (Heartbeater, error) {
	firstErr := make(chan error)
	h := &heartbeater{
		c:       c,
		service: service,
		stop:    make(chan struct{}),
		inst:    inst.Clone(),
	}
	go h.run(firstErr)
	return h, <-firstErr
}

type heartbeater struct {
	c    *Client
	stop chan struct{}

	// Mutex protects inst.Meta
	sync.Mutex
	inst *Instance

	service string
	closed  bool
}

func (h *heartbeater) Close() error {
	if !h.closed {
		close(h.stop)
		h.closed = true
	}
	return nil
}

func (h *heartbeater) SetMeta(meta map[string]string) error {
	h.Lock()
	defer h.Unlock()
	h.inst.Meta = meta
	return h.c.c.Put(fmt.Sprintf("/services/%s/instances/%s", h.service, h.inst.ID), h.inst, nil)
}

const heartbeatInterval = 5 * time.Second

func (h *heartbeater) run(firstErr chan<- error) {
	h.inst.ID = h.inst.id()
	path := fmt.Sprintf("/services/%s/instances/%s", h.service, h.inst.ID)
	register := func() error {
		h.Lock()
		defer h.Unlock()
		return h.c.c.Put(path, h.inst, nil)
	}

	err := register()
	firstErr <- err
	if err != nil {
		return
	}
	ticker := time.NewTicker(heartbeatInterval)
	for {
		select {
		case <-ticker.C:
			if err := register(); err != nil {
				log.Printf("discoverd: heartbeat %s (%s) failed: %s", h.service, h.inst.Addr, err)
			}
		case <-h.stop:
			h.c.c.Delete(path)
			return
		}
	}
}
