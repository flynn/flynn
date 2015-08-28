package main

import (
	"errors"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/host/logmux"
	"github.com/flynn/flynn/pkg/shutdown"
)

func NewDiscoverdManager(backend Backend, mux *logmux.LogMux, hostID, publishAddr string) *DiscoverdManager {
	d := &DiscoverdManager{
		backend: backend,
		mux:     mux,
		inst: &discoverd.Instance{
			Addr: publishAddr,
			Meta: map[string]string{"id": hostID},
		},
	}
	d.local.Store(false)
	return d
}

type DiscoverdManager struct {
	backend Backend
	mux     *logmux.LogMux
	inst    *discoverd.Instance
	mtx     sync.Mutex
	hb      discoverd.Heartbeater
	local   atomic.Value // bool
}

func (d *DiscoverdManager) Close() {
	d.mtx.Lock()
	defer d.mtx.Unlock()
	if d.hb != nil {
		d.hb.Close()
		d.hb = nil
	}
}

func (d *DiscoverdManager) localConnected() bool {
	return d.local.Load().(bool)
}

func (d *DiscoverdManager) heartbeat(url string) error {
	disc := discoverd.NewClientWithURL(url)
	d.mtx.Lock()
	defer d.mtx.Unlock()
	if d.hb != nil {
		d.hb.SetClient(disc)
		return nil
	}
	hb, err := disc.AddServiceAndRegisterInstance("flynn-host", d.inst)
	if err != nil {
		return err
	}
	d.hb = hb
	return nil
}

func (d *DiscoverdManager) ConnectLocal(url string) error {
	if d.localConnected() {
		return errors.New("host: discoverd is already configured")
	}

	if err := d.heartbeat(url); err != nil {
		return err
	}
	d.local.Store(true)

	d.backend.SetDefaultEnv("DISCOVERD", url)

	go func() {
		// give logmux a discoverd client which doesn't use a retry
		// dialer (it has its own reconnect logic)
		disc := discoverd.NewClientWithHTTP(url, http.DefaultClient)
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
		url := fmt.Sprintf("http://%s:1111", ip)
		if err = d.heartbeat(url); err != nil {
			// TODO: log error
			continue
		}
		break
	}
	return err
}
