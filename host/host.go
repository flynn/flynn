package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/technoweenie/grohl"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/host/ports"
	"github.com/flynn/flynn/host/sampi"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/pkg/attempt"
	"github.com/flynn/flynn/pkg/cluster"
	rpc "github.com/flynn/flynn/pkg/rpcplus/comborpc"
)

// Attempts is the attempt strategy that is used to connect to discoverd.
var Attempts = attempt.Strategy{
	Min:   5,
	Total: 10 * time.Second,
	Delay: 200 * time.Millisecond,
}

// A command line flag to accumulate multiple key-value pairs into Metadata,
// e.g. flynn-host -meta foo=bar -meta bar=foo
type MetaFlag map[string]string

func (a MetaFlag) Set(val string) error {
	kv := strings.SplitN(val, "=", 2)
	a[kv[0]] = kv[1]
	return nil
}

func (a MetaFlag) String() string {
	res := make([]string, 0, len(a))
	for k, v := range a {
		res = append(res, k+"="+v)
	}
	return strings.Join(res, ", ")
}

func init() {
	log.SetFlags(log.Lshortfile | log.Lmicroseconds)
}

func main() {
	hostname, _ := os.Hostname()
	externalAddr := flag.String("external", "", "external IP of host")
	bindAddr := flag.String("bind", "", "bind containers to this IP")
	configFile := flag.String("config", "", "configuration file")
	manifestFile := flag.String("manifest", "/etc/flynn-host.json", "manifest file")
	stateFile := flag.String("state", "", "state file")
	hostID := flag.String("id", strings.Replace(hostname, "-", "", -1), "host id")
	force := flag.Bool("force", false, "kill all containers booted by flynn-host before starting")
	volPath := flag.String("volpath", "/var/lib/flynn-host", "directory to create volumes in")
	backendName := flag.String("backend", "libvirt-lxc", "runner backend (docker or libvirt-lxc)")
	metadata := make(MetaFlag)
	flag.Var(&metadata, "meta", "key=value pair to add as metadata")
	flag.Parse()
	grohl.AddContext("app", "host")
	grohl.Log(grohl.Data{"at": "start"})
	g := grohl.NewContext(grohl.Data{"fn": "main"})

	if strings.Contains(*hostID, "-") {
		log.Fatal("host id must not contain dashes")
	}

	portAlloc := map[string]*ports.Allocator{
		"tcp": ports.NewAllocator(55000, 65535),
		"udp": ports.NewAllocator(55000, 65535),
	}

	sh := newShutdownHandler()
	state := NewState()
	var backend Backend
	var err error

	switch *backendName {
	case "libvirt-lxc":
		backend, err = NewLibvirtLXCBackend(state, portAlloc, *volPath, "/tmp/flynn-host-logs", "/usr/bin/flynn-init")
	case "docker":
		backend, err = NewDockerBackend(state, portAlloc, *bindAddr)
	default:
		log.Fatalf("unknown backend %q", *backendName)
	}
	if err != nil {
		log.Fatal(err)
	}

	if err := serveHTTP(&Host{state: state, backend: backend}, &attachHandler{state: state, backend: backend}, sh); err != nil {
		log.Fatal(err)
	}

	if *stateFile != "" {
		sh.BeforeExit(func() { os.Remove(*stateFile) })
		if err := state.Restore(*stateFile, backend); err != nil {
			log.Fatal(err)
		}
	}

	sh.BeforeExit(func() { backend.Cleanup() })

	if *force {
		if err := backend.Cleanup(); err != nil {
			log.Fatal(err)
		}
	}

	runner := &manifestRunner{
		env:          parseEnviron(),
		externalAddr: *externalAddr,
		bindAddr:     *bindAddr,
		backend:      backend,
		state:        state,
		ports:        portAlloc,
	}

	discAddr := os.Getenv("DISCOVERD")
	var disc *discoverd.Client
	if *manifestFile != "" {
		var r io.Reader
		var f *os.File
		if *manifestFile == "-" {
			r = os.Stdin
		} else {
			f, err = os.Open(*manifestFile)
			if err != nil {
				log.Fatal(err)
			}
			r = f
		}
		services, err := runner.runManifest(r)
		if err != nil {
			log.Fatal(err)
		}
		if f != nil {
			f.Close()
		}

		if d, ok := services["discoverd"]; ok {
			discAddr = fmt.Sprintf("%s:%d", d.InternalIP, d.TCPPorts[0])
			var disc *discoverd.Client
			err = Attempts.Run(func() (err error) {
				disc, err = discoverd.NewClientWithAddr(discAddr)
				return
			})
			if err != nil {
				log.Fatal(err)
			}
		}
	}

	if discAddr == "" && *externalAddr != "" {
		discAddr = *externalAddr + ":1111"
	}
	// HACK: use env as global for discoverd connection in sampic
	os.Setenv("DISCOVERD", discAddr)
	if disc == nil {
		disc, err = discoverd.NewClientWithAddr(discAddr)
		if err != nil {
			log.Fatal(err)
		}
	}
	sh.BeforeExit(func() { disc.UnregisterAll() })
	sampiStandby, err := disc.RegisterAndStandby("flynn-host", *externalAddr+":1113", map[string]string{"id": *hostID})
	if err != nil {
		log.Fatal(err)
	}

	// Check if we are the leader so that we can use the cluster functions directly
	sampiCluster := sampi.NewCluster(sampi.NewState())
	select {
	case <-sampiStandby:
		g.Log(grohl.Data{"at": "sampi_leader"})
		rpc.Register(sampiCluster)
	case <-time.After(5 * time.Millisecond):
		go func() {
			<-sampiStandby
			g.Log(grohl.Data{"at": "sampi_leader"})
			rpc.Register(sampiCluster)
		}()
	}
	cluster, err := cluster.NewClientWithSelf(*hostID, NewLocalClient(*hostID, sampiCluster))
	if err != nil {
		log.Fatal(err)
	}
	sh.BeforeExit(func() { cluster.Close() })

	g.Log(grohl.Data{"at": "sampi_connected"})

	events := make(chan host.Event)
	state.AddListener("all", events)
	go syncScheduler(cluster, events)

	h := &host.Host{}
	if *configFile != "" {
		h, err = openConfig(*configFile)
		if err != nil {
			log.Fatal(err)
		}
	}
	if h.Metadata == nil {
		h.Metadata = make(map[string]string)
	}
	for k, v := range metadata {
		h.Metadata[k] = v
	}
	h.ID = *hostID

	for {
		newLeader := cluster.NewLeaderSignal()

		h.Jobs = state.ClusterJobs()
		jobs := make(chan *host.Job)
		hostErr := cluster.RegisterHost(h, jobs)
		g.Log(grohl.Data{"at": "host_registered"})
		for job := range jobs {
			if *externalAddr != "" {
				if job.Config.Env == nil {
					job.Config.Env = make(map[string]string)
				}
				job.Config.Env["EXTERNAL_IP"] = *externalAddr
				job.Config.Env["DISCOVERD"] = discAddr
			}
			if err := backend.Run(job); err != nil {
				state.SetStatusFailed(job.ID, err)
			}
		}
		g.Log(grohl.Data{"at": "sampi_disconnected", "err": *hostErr})

		<-newLeader
	}
}

type sampiClient interface {
	ConnectHost(*host.Host, chan *host.Job) *error
	RemoveJobs([]string) error
}

type sampiSyncClient interface {
	RemoveJobs([]string) error
}

func syncScheduler(scheduler sampiSyncClient, events <-chan host.Event) {
	for event := range events {
		if event.Event != "stop" {
			continue
		}
		grohl.Log(grohl.Data{"fn": "scheduler_event", "at": "remove_job", "job.id": event.JobID})
		if err := scheduler.RemoveJobs([]string{event.JobID}); err != nil {
			grohl.Log(grohl.Data{"fn": "scheduler_event", "at": "remove_job", "status": "error", "err": err, "job.id": event.JobID})
		}
	}
}

func newShutdownHandler() *shutdownHandler {
	s := &shutdownHandler{done: make(chan struct{})}
	go s.wait()
	return s
}

type shutdownHandler struct {
	mtx  sync.RWMutex
	done chan struct{}
}

func (h *shutdownHandler) BeforeExit(f func()) {
	h.mtx.RLock()
	go func() {
		<-h.done
		f()
		h.mtx.RUnlock()
	}()
}

// kill all jobs

func (h *shutdownHandler) wait() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, os.Signal(syscall.SIGTERM))
	sig := <-ch
	grohl.Log(grohl.Data{"fn": "shutdown", "at": "start", "signal": fmt.Sprint(sig)})
	// signal exit handlers
	close(h.done)
	// wait for exit handlers to finish
	h.mtx.Lock()
	os.Exit(0)
}
