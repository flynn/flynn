package discoverd

import (
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	hh "github.com/flynn/flynn/pkg/httphelper"
)

type Heartbeater interface {
	SetMeta(map[string]string) error
	Close() error
	Addr() string
}

func (c *Client) maybeAddService(service string) error {
	if err := c.AddService(service); err != nil {
		if je, ok := err.(hh.JSONError); !ok || je.Code != hh.ObjectExistsError {
			return err
		}
	}
	return nil
}

func (c *Client) AddServiceAndRegister(service, addr string) (Heartbeater, error) {
	if err := c.maybeAddService(service); err != nil {
		return nil, err
	}
	return c.Register(service, addr)
}

func (c *Client) AddServiceAndRegisterInstance(service string, inst *Instance) (Heartbeater, error) {
	if err := c.maybeAddService(service); err != nil {
		return nil, err
	}
	return c.RegisterInstance(service, inst)
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
	h.inst.Addr = expandAddr(h.inst.Addr)
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

func (h *heartbeater) Addr() string {
	return h.inst.Addr
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

var externalIP = os.Getenv("EXTERNAL_IP")

func expandAddr(addr string) string {
	if addr[0] == ':' {
		return os.Getenv("EXTERNAL_IP") + addr
	}
	return addr
}
