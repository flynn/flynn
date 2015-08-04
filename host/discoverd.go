package main

import (
	"errors"
	"fmt"
	"sync"

	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/host/logmux"
	"github.com/flynn/flynn/pkg/shutdown"
)

func NewDiscoverdManager(backend Backend, mux *logmux.LogMux, hostID, publishAddr string) *DiscoverdManager {
	return &DiscoverdManager{
		backend: backend,
		mux:     mux,
		inst: &discoverd.Instance{
			Addr: publishAddr,
			Meta: map[string]string{"id": hostID},
		},
	}
}

type DiscoverdManager struct {
	backend Backend
	mux     *logmux.LogMux
	inst    *discoverd.Instance
	mtx     sync.Mutex
	local   discoverd.Heartbeater
	peer    discoverd.Heartbeater
}

func (d *DiscoverdManager) Close() {
	d.mtx.Lock()
	defer d.mtx.Unlock()
	if d.local != nil {
		d.local.Close()
		d.local = nil
	}
	if d.peer != nil {
		d.peer.Close()
		d.peer = nil
	}
}

func (d *DiscoverdManager) localConnected() bool {
	d.mtx.Lock()
	defer d.mtx.Unlock()
	return d.local != nil
}

func (d *DiscoverdManager) ConnectLocal(url string) error {
	if d.localConnected() {
		return errors.New("host: discoverd is already configured")
	}
	disc := discoverd.NewClientWithURL(url)
	hb, err := disc.AddServiceAndRegisterInstance("flynn-host", d.inst)
	if err != nil {
		return err
	}

	d.mtx.Lock()
	if d.peer != nil {
		d.peer.Close()
	}
	d.local = hb
	d.mtx.Unlock()

	d.backend.SetDefaultEnv("DISCOVERD", url)

	go func() {
		if err := d.mux.Connect(disc, "logaggregator"); err != nil {
			shutdown.Fatal(err)
		}
	}()

	return nil
}

func (d *DiscoverdManager) ConnectPeer(ips []string) error {
	if d.localConnected() {
		return nil
	}
	if len(ips) == 0 {
		return errors.New("host: no discoverd peers available")
	}

	var err error
	for _, ip := range ips {
		// TODO: log attempt
		disc := discoverd.NewClientWithURL(fmt.Sprintf("http://%s:1111", ip))
		var hb discoverd.Heartbeater
		hb, err = disc.AddServiceAndRegisterInstance("flynn-host", d.inst)
		if err != nil {
			// TODO: log error
			continue
		}
		d.mtx.Lock()
		if d.local != nil {
			hb.Close()
		} else {
			d.peer = hb
		}
		d.mtx.Unlock()
		break
	}
	return err
}
