package health

import (
	"errors"
	"fmt"
	"time"

	. "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/random"
	"github.com/flynn/flynn/pkg/stream"
)

type RegisterSuite struct{}

var _ = Suite(&RegisterSuite{})

type RegistrarFunc func(service string, inst *discoverd.Instance) (discoverd.Heartbeater, error)

func (f RegistrarFunc) RegisterInstance(service string, inst *discoverd.Instance) (discoverd.Heartbeater, error) {
	return f(service, inst)
}

type FakeHeartbeat struct {
	closeFn   func() error
	setMetaFn func(map[string]string) error
	addrFn    func() string
}

func (f FakeHeartbeat) SetMeta(meta map[string]string) error { return f.setMetaFn(meta) }
func (f FakeHeartbeat) Close() error                         { return f.closeFn() }
func (f FakeHeartbeat) Addr() string                         { return f.addrFn() }
func (f FakeHeartbeat) SetClient(*discoverd.Client)          {}

func init() {
	registerErrWait = time.Millisecond
}

func (RegisterSuite) TestRegister(c *C) {
	type step struct {
		event      bool // send an event
		up         bool // event type
		register   bool // event should trigger register
		unregister bool // event should unregister service
		setMeta    bool // attempt SetMeta
		success    bool // true if SetMeta or Register should succeed
	}

	type called struct {
		args      map[string]interface{}
		returnVal chan bool
	}

	run := func(c *C, steps []step) {
		check := CheckFunc(func() error { return nil })

		metaChan := make(chan called)
		unregisterChan := make(chan called)
		heartbeater := FakeHeartbeat{
			addrFn: func() string {
				return "notnil"
			},
			closeFn: func() error {
				unregisterChan <- called{}
				return nil
			},
			setMetaFn: func(meta map[string]string) error {
				success := make(chan bool)
				metaChan <- called{
					args: map[string]interface{}{
						"meta": meta,
					},
					returnVal: success,
				}
				if !<-success {
					return errors.New("SetMeta failed")
				}
				return nil
			},
		}

		registrarChan := make(chan called)
		registrar := RegistrarFunc(func(service string, inst *discoverd.Instance) (discoverd.Heartbeater, error) {
			success := make(chan bool)
			registrarChan <- called{
				args: map[string]interface{}{
					"service": service,
					"inst":    inst,
				},
				returnVal: success,
			}
			defer func() { registrarChan <- called{} }()
			if <-success {
				return heartbeater, nil
			}
			return nil, errors.New("registrar failure")
		})

		monitorChan := make(chan bool)
		monitor := func(c Check, ch chan MonitorEvent) stream.Stream {
			stream := stream.New()
			go func() {
				defer close(ch)
				for {
					select {
					case up, ok := <-monitorChan:
						if !ok {
							return
						}
						if up {
							ch <- MonitorEvent{
								Check:  check,
								Status: MonitorStatusUp,
							}
						} else {
							ch <- MonitorEvent{
								Check:  check,
								Status: MonitorStatusDown,
							}
						}
					case <-stream.StopCh:
						return
					}
				}
			}()
			return stream
		}

		reg := Registration{
			Service: "test",
			Instance: &discoverd.Instance{
				Meta: make(map[string]string),
			},
			Registrar: registrar,
			Monitor:   monitor,
			Check:     check,
			Events:    make(chan MonitorEvent),
		}
		hb := reg.Register()
		defer func() {
			go func() { <-unregisterChan }()
			hb.Close()
			close(unregisterChan)
		}()

		errCh := make(chan bool)
		errCheck := func(ch chan called, stop chan struct{}) {
			go func() {
				select {
				case _, ok := <-ch:
					if !ok {
						return
					}
					errCh <- true
				case <-stop:
					errCh <- false
				}
			}()
		}

		var stop chan struct{}
		currentMeta := make(map[string]string)
		for _, step := range steps {
			stop = make(chan struct{})
			var wait int

			if step.event && !step.unregister {
				monitorChan <- step.up
			}
			if step.register {
				call := <-registrarChan
				c.Assert(call.args["inst"].(*discoverd.Instance).Meta, DeepEquals, currentMeta)
				call.returnVal <- step.success
				<-registrarChan
				if !step.success {
					// keep returning error from register in case we hit the retry
					close(registrarChan)
				}
			} else {
				wait++
				errCheck(registrarChan, stop)
			}
			if step.unregister {
				// before unregistering, Addr should not be nil
				c.Assert(hb.Addr(), Not(Equals), "")
				monitorChan <- false
				select {
				case <-unregisterChan:
				case <-time.After(3 * time.Second):
					c.Error("Timed out waiting for unregistration")
				}
				// Addr should be nil now
				c.Assert(hb.Addr(), Equals, "")
			} else {
				wait++
				errCheck(unregisterChan, stop)
			}
			if step.setMeta {
				go func() {
					call := <-metaChan
					if call.returnVal != nil {
						call.returnVal <- step.success
					}
				}()
				// SetMeta needs to succeed every time, regardless of the situation
				currentMeta["TEST"] = random.UUID()
				c.Assert(hb.SetMeta(currentMeta), IsNil)
			} else {
				wait++
				errCheck(metaChan, stop)
			}

			// check event forwarding
			if step.event {
				select {
				case val := <-reg.Events:
					if step.up {
						c.Assert(val.Status, Equals, MonitorStatusUp)
					} else {
						c.Assert(val.Status, Equals, MonitorStatusDown)
					}
				case <-time.After(3 * time.Second):
					c.Error("Timed out waiting for Registration.Events")
				}
			}

			// check whether functions were called on this step that should not
			// have ran
			close(stop)
			for i := 0; i < wait; i++ {
				select {
				case err := <-errCh:
					if err {
						c.Error("Received data on a channel that should not be recieving on this step")
					}
				}
			}
		}
		close(monitorChan)
		close(metaChan)
	}

	for _, t := range []struct {
		name  string
		steps []step
	}{
		{
			name: "register success up/down/up",
			steps: []step{
				{event: true, up: true, register: true, success: true},
				{event: true, unregister: true},
				{event: true, up: true, register: true, success: true},
			},
		},
		{
			name: "register fails then succeeds",
			steps: []step{
				{event: true, up: true, register: true, success: false},
				{register: true, success: true},
			},
		},
		{
			name: "register is called only once if we get two up events",
			steps: []step{
				{event: true, up: true, register: true, success: true},
				{event: true, up: true, register: false},
			},
		},
		{
			name: "setmeta while registered",
			steps: []step{
				{event: true, up: true, register: true, success: true},
				{setMeta: true, success: true},
			},
		},
		{
			name: "setmeta while offline",
			steps: []step{
				{setMeta: true, success: false},
				// confirm that the right meta is sent when the process does
				// come up
				{event: true, up: true, register: true, success: true},
			},
		},
		{
			name: "setmeta while erroring registration",
			steps: []step{
				{event: true, up: true, register: true, success: false},
				{register: true, success: false},
				{setMeta: true, success: false},
				{register: true, success: true},
			},
		},
		{
			name: "register failing then offline",
			steps: []step{
				{event: true, up: true, register: true, success: false},
				{register: true, success: false},
				{event: true},
				{}, // make sure register does not run
				{},
			},
		},
	} {
		fmt.Println("--- TEST:", t.name)
		run(c, t.steps)
	}
}
