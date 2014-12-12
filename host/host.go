package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-docopt"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/technoweenie/grohl"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/host/cli"
	"github.com/flynn/flynn/host/config"
	"github.com/flynn/flynn/host/ports"
	"github.com/flynn/flynn/host/sampi"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/pkg/attempt"
	"github.com/flynn/flynn/pkg/cluster"
	rpc "github.com/flynn/flynn/pkg/rpcplus/comborpc"
	"github.com/flynn/flynn/pkg/shutdown"
)

// discoverdAttempts is the attempt strategy that is used to connect to discoverd.
var discoverdAttempts = attempt.Strategy{
	Min:   5,
	Total: 10 * time.Minute,
	Delay: 200 * time.Millisecond,
}

const configFile = "/etc/flynn/host.json"

func init() {
	log.SetFlags(log.Lshortfile | log.Lmicroseconds)

	cli.Register("daemon", runDaemon, `
usage: flynn-host daemon [options] [--meta=<KEY=VAL>...]

options:
  --external=IP          external IP of host
  --manifest=PATH        path to manifest file [default: /etc/flynn-host.json]
  --state=PATH           path to state file [default: /var/lib/flynn/host-state.bolt]
  --id=ID                host id
  --force                kill all containers booted by flynn-host before starting
  --volpath=PATH         directory to create volumes in [default: /var/lib/flynn/host-volumes]
  --backend=BACKEND      runner backend [default: libvirt-lxc]
  --meta=<KEY=VAL>...    key=value pair to add as metadata
  --bind=IP              bind containers to IP
  --flynn-init=PATH      path to flynn-init binary [default: /usr/bin/flynn-init]
	`)
}

func main() {
	usage := `usage: flynn-host [-h|--help] <command> [<args>...]

Options:
  -h, --help                 Show this message

Commands:
  help                       Show usage for a specific command
  init                       Create cluster configuration for daemon
  daemon                     Start the daemon
  download                   Download container images
  bootstrap                  Bootstrap layer 1
  inspect                    Get low-level information about a job
  log                        Get the logs of a job
  ps                         List jobs
  stop                       Stop running jobs
  upload-debug-info          Upload debug information to an anonymous gist

See 'flynn-host help <command>' for more information on a specific command.
`

	args, _ := docopt.Parse(usage, nil, true, "", true)
	cmd := args.String["<command>"]
	cmdArgs := args.All["<args>"].([]string)

	if cmd == "help" {
		if len(cmdArgs) == 0 { // `flynn help`
			fmt.Println(usage)
			return
		} else { // `flynn help <command>`
			cmd = cmdArgs[0]
			cmdArgs = []string{"--help"}
		}
	}

	if cmd == "daemon" {
		// merge in args and env from config file, if available
		var c *config.Config
		var err error
		if n := os.Getenv("FLYNN_HOST_CONFIG"); n != "" {
			c, err = config.Open(n)
			if err != nil {
				log.Fatalf("error opening config file %s: %s", n, err)
			}
		} else {
			c, err = config.Open(configFile)
			if err != nil && !os.IsNotExist(err) {
				log.Fatalf("error opening config file %s: %s", configFile, err)
			}
			if c == nil {
				c = &config.Config{}
			}
		}
		cmdArgs = append(cmdArgs, c.Args...)
		for k, v := range c.Env {
			os.Setenv(k, v)
		}
	}

	if err := cli.Run(cmd, cmdArgs); err != nil {
		log.Fatal(err)
	}
}

func runDaemon(args *docopt.Args) {
	hostname, _ := os.Hostname()
	externalAddr := args.String["--external"]
	bindAddr := args.String["--bind"]
	manifestFile := args.String["--manifest"]
	stateFile := args.String["--state"]
	hostID := args.String["--id"]
	force := args.Bool["--force"]
	volPath := args.String["--volpath"]
	backendName := args.String["--backend"]
	flynnInit := args.String["--flynn-init"]
	metadata := args.All["--meta"].([]string)

	grohl.AddContext("app", "host")
	grohl.Log(grohl.Data{"at": "start"})
	g := grohl.NewContext(grohl.Data{"fn": "main"})

	if hostID == "" {
		hostID = strings.Replace(hostname, "-", "", -1)
	}
	if strings.Contains(hostID, "-") {
		log.Fatal("host id must not contain dashes")
	}
	if externalAddr == "" {
		var err error
		externalAddr, err = config.DefaultExternalIP()
		if err != nil {
			log.Fatal(err)
		}
	}

	portAlloc := map[string]*ports.Allocator{
		"tcp": ports.NewAllocator(55000, 65535),
		"udp": ports.NewAllocator(55000, 65535),
	}

	sh := shutdown.NewHandler()
	state := NewState(hostID, stateFile)
	var backend Backend
	var err error

	switch backendName {
	case "libvirt-lxc":
		backend, err = NewLibvirtLXCBackend(state, portAlloc, volPath, "/tmp/flynn-host-logs", flynnInit)
	default:
		log.Fatalf("unknown backend %q", backendName)
	}
	if err != nil {
		sh.Fatal(err)
	}

	if err := serveHTTP(&Host{state: state, backend: backend}, &attachHandler{state: state, backend: backend}, sh); err != nil {
		sh.Fatal(err)
	}

	if err := state.Restore(backend); err != nil {
		sh.Fatal(err)
	}

	var jobStream cluster.Stream
	sh.BeforeExit(func() {
		if jobStream != nil {
			jobStream.Close()
		}
		backend.Cleanup()
	})

	if force {
		if err := backend.Cleanup(); err != nil {
			sh.Fatal(err)
		}
	}

	runner := &manifestRunner{
		env:          parseEnviron(),
		externalAddr: externalAddr,
		bindAddr:     bindAddr,
		backend:      backend,
		state:        state,
		ports:        portAlloc,
	}

	discAddr := os.Getenv("DISCOVERD")
	var disc *discoverd.Client
	if manifestFile != "" {
		var r io.Reader
		var f *os.File
		if manifestFile == "-" {
			r = os.Stdin
		} else {
			f, err = os.Open(manifestFile)
			if err != nil {
				sh.Fatal(err)
			}
			r = f
		}
		services, err := runner.runManifest(r)
		if err != nil {
			sh.Fatal(err)
		}
		if f != nil {
			f.Close()
		}

		if d, ok := services["discoverd"]; ok {
			discAddr = fmt.Sprintf("%s:%d", d.ExternalIP, d.TCPPorts[0])
			var disc *discoverd.Client
			err = discoverdAttempts.Run(func() (err error) {
				disc, err = discoverd.NewClientWithAddr(discAddr)
				return
			})
			if err != nil {
				sh.Fatal(err)
			}
		}
	}

	if discAddr == "" && externalAddr != "" {
		discAddr = externalAddr + ":1111"
	}
	// HACK: use env as global for discoverd connection in sampic
	os.Setenv("DISCOVERD", discAddr)
	if disc == nil {
		disc, err = discoverd.NewClientWithAddr(discAddr)
		if err != nil {
			sh.Fatal(err)
		}
	}
	sh.BeforeExit(func() { disc.UnregisterAll() })
	sampiStandby, err := disc.RegisterAndStandby("flynn-host", externalAddr+":1113", map[string]string{"id": hostID})
	if err != nil {
		sh.Fatal(err)
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
	cluster, err := cluster.NewClientWithSelf(hostID, NewLocalClient(hostID, sampiCluster))
	if err != nil {
		sh.Fatal(err)
	}
	sh.BeforeExit(func() { cluster.Close() })

	g.Log(grohl.Data{"at": "sampi_connected"})

	events := state.AddListener("all")
	go syncScheduler(cluster, events)

	h := &host.Host{}
	if h.Metadata == nil {
		h.Metadata = make(map[string]string)
	}
	for _, s := range metadata {
		kv := strings.SplitN(s, "=", 2)
		h.Metadata[kv[0]] = kv[1]
	}
	h.ID = hostID

	for {
		newLeader := cluster.NewLeaderSignal()

		h.Jobs = state.ClusterJobs()
		jobs := make(chan *host.Job)
		jobStream = cluster.RegisterHost(h, jobs)
		g.Log(grohl.Data{"at": "host_registered"})
		for job := range jobs {
			if externalAddr != "" {
				if job.Config.Env == nil {
					job.Config.Env = make(map[string]string)
				}
				job.Config.Env["EXTERNAL_IP"] = externalAddr
				job.Config.Env["DISCOVERD"] = discAddr
			}
			if err := backend.Run(job); err != nil {
				state.SetStatusFailed(job.ID, err)
			}
		}
		g.Log(grohl.Data{"at": "sampi_disconnected", "err": jobStream.Err})

		// if the process is shutting down, just block
		if sh.Active {
			<-make(chan struct{})
		}

		<-newLeader
	}
}

func syncScheduler(scheduler *cluster.Client, events <-chan host.Event) {
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
