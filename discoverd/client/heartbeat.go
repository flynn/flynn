package discoverd

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/flynn/flynn/pkg/attempt"
	hh "github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/random"
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
	SetClient(*Client)
}

var runAttempts = attempt.Strategy{
	Min:   5,
	Total: 60 * time.Second,
	Delay: 200 * time.Millisecond,
}

func (c *Client) maybeAddService(service string) error {
	err := c.AddService(service, nil)
	if hh.IsObjectExistsError(err) {
		return nil
	}
	return err
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
	h := newHeartbeater(c, service, inst)
	err := runAttempts.Run(func() error {
		firstErr := make(chan error)
		go h.run(firstErr)
		return <-firstErr
	})
	if err != nil {
		return nil, err
	}

	return h, nil
}

func newHeartbeater(c *Client, service string, inst *Instance) *heartbeater {
	h := &heartbeater{
		service: service,
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
		inst:    inst.Clone(),
	}
	h.c.Store(c)
	return h
}

type heartbeater struct {
	c atomic.Value // *Client

	stop chan struct{}
	done chan struct{}

	// Mutex protects inst.Meta
	sync.Mutex
	inst *Instance

	service   string
	closeOnce sync.Once
}

func (h *heartbeater) Close() error {
	h.closeOnce.Do(func() {
		close(h.stop)
		<-h.done
	})
	return nil
}

func (h *heartbeater) SetMeta(meta map[string]string) error {
	h.Lock()
	defer h.Unlock()
	h.inst.Meta = meta
	return h.client().Put(fmt.Sprintf("/services/%s/instances/%s", h.service, h.inst.ID), h.inst, nil)
}

func (h *heartbeater) Addr() string {
	return h.inst.Addr
}

func (h *heartbeater) SetClient(c *Client) {
	h.c.Store(c)
}

func (h *heartbeater) client() *Client {
	return h.c.Load().(*Client)
}

const (
	heartbeatInterval        = 5 * time.Second
	heartbeatJitter          = 2 * time.Second
	heartbeatFailingInterval = 200 * time.Millisecond
)

func (h *heartbeater) run(firstErr chan<- error) {
	path := fmt.Sprintf("/services/%s/instances/%s", h.service, h.inst.ID)
	register := func() error {
		h.Lock()
		defer h.Unlock()
		return h.client().Put(path, h.inst, nil)
	}

	err := register()
	firstErr <- err
	if err != nil {
		return
	}
	timer := time.NewTimer(nextHeartbeat())
	for {
		select {
		case <-timer.C:
			if err := register(); err != nil {
				h.client().Logger.Error("heartbeat failed", "service", h.service, "addr", h.inst.Addr, "err", err)
				timer.Reset(nextHeartbeatFailing())
				break
			}
			timer.Reset(nextHeartbeat())
		case <-h.stop:
			h.client().Delete(path)
			close(h.done)
			return
		}
	}
}

func expandAddr(addr string) string {
	if addr[0] == ':' {
		return os.Getenv("EXTERNAL_IP") + addr
	}
	return addr
}

func nextHeartbeat() time.Duration {
	return heartbeatInterval - time.Duration(random.Math.Int63n(int64(heartbeatJitter)))
}

func nextHeartbeatFailing() time.Duration {
	return heartbeatFailingInterval + time.Duration(random.Math.Int63n(int64(heartbeatFailingInterval)))
}
