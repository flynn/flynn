package discoverd

import (
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	hh "github.com/flynn/flynn/pkg/httphelper"
)

// EnvInstanceMeta are environment variables which will be automatically added
// to instance metadata if present.
var EnvInstanceMeta = map[string]struct{}{
	"FLYNN_APP_ID":       {},
	"FLYNN_RELEASE_ID":   {},
	"FLYNN_PROCESS_TYPE": {},
	"FLYNN_JOB_ID":       {},
}

type Heartbeater interface {
	SetMeta(map[string]string) error
	Close() error
	Addr() string
}

func (c *Client) maybeAddService(service string) error {
	if err := c.AddService(service, nil); err != nil {
		if !hh.IsObjectExistsError(err) {
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
	return c.RegisterInstance(service, &Instance{Addr: addr})
}

func (c *Client) RegisterInstance(service string, inst *Instance) (Heartbeater, error) {
	inst.Addr = expandAddr(inst.Addr)
	if inst.Proto == "" {
		inst.Proto = "tcp"
	}
	inst.ID = inst.id()
	// add EnvInstanceMeta if present
	for _, env := range os.Environ() {
		kv := strings.SplitN(env, "=", 2)
		if _, ok := EnvInstanceMeta[kv[0]]; !ok {
			continue
		}
		if inst.Meta == nil {
			inst.Meta = make(map[string]string)
		}
		inst.Meta[kv[0]] = kv[1]
	}
	h := &heartbeater{
		c:       c,
		service: service,
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
		inst:    inst.Clone(),
	}
	firstErr := make(chan error)
	go h.run(firstErr)
	return h, <-firstErr
}

type heartbeater struct {
	c    *Client
	stop chan struct{}
	done chan struct{}

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
		<-h.done
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
			close(h.done)
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
