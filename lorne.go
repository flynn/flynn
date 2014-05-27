package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"

	"github.com/flynn/flynn-host/sampi"
	"github.com/flynn/flynn-host/types"
	"github.com/flynn/go-discoverd"
	"github.com/flynn/go-flynn/attempt"
	"github.com/flynn/go-flynn/cluster"
	rpc "github.com/flynn/rpcplus/comborpc"
	"github.com/technoweenie/grohl"
)

// Attempts is the attempt strategy that is used to connect to discoverd.
var Attempts = attempt.Strategy{
	Min:   5,
	Total: 5 * time.Second,
	Delay: 200 * time.Millisecond,
}

// A command line flag to accumulate multiple key-value pairs into Attributes,
// e.g. flynn-host -attribute foo=bar -attribute bar=foo
type AttributeFlag map[string]string

func (a AttributeFlag) Set(val string) error {
	kv := strings.SplitN(val, "=", 2)
	a[kv[0]] = kv[1]
	return nil
}

func (a AttributeFlag) String() string {
	res := make([]string, 0, len(a))
	for k, v := range a {
		res = append(res, k+"="+v)
	}
	return strings.Join(res, ", ")
}

func main() {
	hostname, _ := os.Hostname()
	externalAddr := flag.String("external", "", "external IP of host")
	bindAddr := flag.String("bind", "", "bind containers to this IP")
	configFile := flag.String("config", "", "configuration file")
	manifestFile := flag.String("manifest", "/etc/flynn-host.json", "manifest file")
	hostID := flag.String("id", hostname, "host id")
	force := flag.Bool("force", false, "kill all containers booted by flynn-host before starting")
	attributes := make(AttributeFlag)
	flag.Var(&attributes, "attribute", "key=value pair to add as an attribute")
	flag.Parse()
	grohl.AddContext("app", "lorne")
	grohl.Log(grohl.Data{"at": "start"})
	g := grohl.NewContext(grohl.Data{"fn": "main"})

	state := NewState()
	backend, err := NewDockerBackend(state, *bindAddr)
	if err != nil {
		log.Fatal(err)
	}

	go serveHTTP(&Host{state: state}, &attachHandler{state: state, backend: backend})

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
	if h.Attributes == nil {
		h.Attributes = make(map[string]string)
	}
	for k, v := range attributes {
		h.Attributes[k] = v
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
				job.Config.Env = appendUnique(job.Config.Env, "EXTERNAL_IP="+*externalAddr, "DISCOVERD="+discAddr)
			}
			backend.Run(job)
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
