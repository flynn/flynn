package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-docopt"
	"github.com/flynn/flynn/Godeps/_workspace/src/gopkg.in/inconshreveable/log15.v2"
	"github.com/flynn/flynn/bootstrap/discovery"
	"github.com/flynn/flynn/host/cli"
	"github.com/flynn/flynn/host/config"
	"github.com/flynn/flynn/host/logmux"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/host/volume"
	"github.com/flynn/flynn/host/volume/manager"
	zfsVolume "github.com/flynn/flynn/host/volume/zfs"
	"github.com/flynn/flynn/pkg/shutdown"
	"github.com/flynn/flynn/pkg/version"
)

const configFile = "/etc/flynn/host.json"

var logger = log15.New("app", "host", "pid", os.Getpid())

func init() {
	cli.Register("daemon", runDaemon, `
usage: flynn-host daemon [options]

options:
  --http-port=PORT       HTTP port [default: 1113]
  --external-ip=IP       external IP of host
  --listen-ip=IP         bind host network services to this IP
  --state=PATH           path to state file [default: /var/lib/flynn/host-state.bolt]
  --id=ID                host id
  --force                kill all containers booted by flynn-host before starting
  --volpath=PATH         directory to create volumes in [default: /var/lib/flynn/volumes]
  --vol-provider=VOL     volume provider [default: zfs]
  --backend=BACKEND      runner backend [default: libvirt-lxc]
  --flynn-init=PATH      path to flynn-init binary [default: /usr/local/bin/flynn-init]
  --nsumount=PATH        path to flynn-nsumount binary [default: /usr/local/bin/flynn-nsumount]
  --log-dir=DIR          directory to store job logs [default: /var/log/flynn]
  --discovery=TOKEN      join cluster with discovery token
  --peer-ips=IPLIST      join existing cluster using IPs
  --bridge-name=NAME     network bridge name [default: flynnbr0]
	`)
}

func main() {
	defer shutdown.Exit()

	usage := `usage: flynn-host [-h|--help] [--version] <command> [<args>...]

Options:
  -h, --help                 Show this message
  --version                  Show current version

Commands:
  help                       Show usage for a specific command
  init                       Create cluster configuration for daemon
  daemon                     Start the daemon
  update                     Update Flynn components
  download                   Download container images
  bootstrap                  Bootstrap layer 1
  inspect                    Get low-level information about a job
  log                        Get the logs of a job
  ps                         List jobs
  stop                       Stop running jobs
  signal                     Signal a job
  destroy-volumes            Destroys the local volume database
  collect-debug-info         Collect debug information into an anonymous gist or tarball
  list                       Lists ID and IP of each host
  version                    Show current version

See 'flynn-host help <command>' for more information on a specific command.
`

	args, _ := docopt.Parse(usage, nil, true, version.String(), true)
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
		if n := os.Getenv("FLYNN_HOST_CONFIG"); n != "" {
			var err error
			c, err = config.Open(n)
			if err != nil {
				shutdown.Fatalf("error opening config file %s: %s", n, err)
			}
		} else {
			var err error
			c, err = config.Open(configFile)
			if err != nil && !os.IsNotExist(err) {
				shutdown.Fatalf("error opening config file %s: %s", configFile, err)
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
		if err == cli.ErrInvalidCommand {
			fmt.Printf("ERROR: %q is not a valid command\n\n", cmd)
			fmt.Println(usage)
			shutdown.ExitWithCode(1)
		}
		shutdown.Fatal(err)
	}
}

func runDaemon(args *docopt.Args) {
	hostname, _ := os.Hostname()
	httpPort := args.String["--http-port"]
	externalIP := args.String["--external-ip"]
	listenIP := args.String["--listen-ip"]
	stateFile := args.String["--state"]
	hostID := args.String["--id"]
	force := args.Bool["--force"]
	volPath := args.String["--volpath"]
	volProvider := args.String["--vol-provider"]
	backendName := args.String["--backend"]
	flynnInit := args.String["--flynn-init"]
	nsumount := args.String["--nsumount"]
	logDir := args.String["--log-dir"]
	discoveryToken := args.String["--discovery"]
	bridgeName := args.String["--bridge-name"]

	var peerIPs []string
	if args.String["--peer-ips"] != "" {
		peerIPs = strings.Split(args.String["--peer-ips"], ",")
	}

	if hostID == "" {
		hostID = strings.Replace(hostname, "-", "", -1)
	}

	log := logger.New("fn", "runDaemon", "host.id", hostID)
	log.Info("starting daemon")

	log.Info("validating host ID")
	if strings.Contains(hostID, "-") {
		shutdown.Fatal("host id must not contain dashes")
	}
	if externalIP == "" {
		log.Info("detecting external IP")
		var err error
		externalIP, err = config.DefaultExternalIP()
		if err != nil {
			log.Error("error detecting external IP", "err", err)
			shutdown.Fatal(err)
		}
		log.Info("using external IP " + externalIP)
	}

	publishAddr := net.JoinHostPort(externalIP, httpPort)
	if discoveryToken != "" {
		// TODO: retry
		log.Info("registering with cluster discovery service", "token", discoveryToken, "addr", publishAddr, "name", hostID)
		discoveryID, err := discovery.RegisterInstance(discovery.Info{
			ClusterURL:  discoveryToken,
			InstanceURL: "http://" + publishAddr,
			Name:        hostID,
		})
		if err != nil {
			log.Error("error registering with cluster discovery service", "err", err)
			shutdown.Fatal(err)
		}
		log.Info("registered with cluster discovery service", "id", discoveryID)
	}

	state := NewState(hostID, stateFile)
	shutdown.BeforeExit(func() { state.CloseDB() })

	log.Info("initializing volume manager", "provider", volProvider)
	var newVolProvider func() (volume.Provider, error)
	switch volProvider {
	case "zfs":
		newVolProvider = func() (volume.Provider, error) {
			// use a zpool backing file size of either 70% of the device on which
			// volumes will reside, or 100GB if that can't be determined.
			log.Info("determining ZFS zpool size")
			var size int64
			var dev syscall.Statfs_t
			if err := syscall.Statfs(volPath, &dev); err == nil {
				size = (dev.Bsize * int64(dev.Blocks) * 7) / 10
			} else {
				size = 100000000000
			}
			log.Info(fmt.Sprintf("using ZFS zpool size %d", size))

			return zfsVolume.NewProvider(&zfsVolume.ProviderConfig{
				DatasetName: "flynn-default",
				Make: &zfsVolume.MakeDev{
					BackingFilename: filepath.Join(volPath, "zfs/vdev/flynn-default-zpool.vdev"),
					Size:            size,
				},
				WorkingDir: filepath.Join(volPath, "zfs"),
			})
		}
	case "mock":
		newVolProvider = func() (volume.Provider, error) { return nil, nil }
	default:
		shutdown.Fatalf("unknown volume provider: %q", volProvider)
	}
	vman := volumemanager.New(
		filepath.Join(volPath, "volumes.bolt"),
		newVolProvider,
	)
	shutdown.BeforeExit(func() { vman.CloseDB() })

	mux := logmux.New(hostID, logDir, logger.New("host.id", hostID, "component", "logmux"))

	log.Info("initializing job backend", "type", backendName)
	var backend Backend
	var err error
	switch backendName {
	case "libvirt-lxc":
		backend, err = NewLibvirtLXCBackend(state, vman, bridgeName, flynnInit, nsumount, mux)
	case "mock":
		backend = MockBackend{}
	default:
		shutdown.Fatalf("unknown backend %q", backendName)
	}
	if err != nil {
		shutdown.Fatal(err)
	}
	backend.SetDefaultEnv("EXTERNAL_IP", externalIP)
	backend.SetDefaultEnv("LISTEN_IP", listenIP)

	var buffers host.LogBuffers
	discoverdManager := NewDiscoverdManager(backend, mux, hostID, publishAddr)
	publishURL := "http://" + publishAddr
	host := &Host{
		id:               hostID,
		url:              publishURL,
		status:           &host.HostStatus{ID: hostID, PID: os.Getpid(), URL: publishURL},
		state:            state,
		backend:          backend,
		vman:             vman,
		connectDiscoverd: discoverdManager.ConnectLocal,
		log:              logger.New("host.id", hostID),
	}

	// restore the host status if set in the environment
	if statusEnv := os.Getenv("FLYNN_HOST_STATUS"); statusEnv != "" {
		log.Info("restoring host status from parent")
		if err := json.Unmarshal([]byte(statusEnv), &host.status); err != nil {
			log.Error("error restoring host status from parent", "err", err)
			shutdown.Fatal(err)
		}
		pid := os.Getpid()
		log.Info("setting status PID", "pid", pid)
		host.status.PID = pid
	}

	log.Info("creating HTTP listener")
	l, err := newHTTPListener(net.JoinHostPort(listenIP, httpPort))
	if err != nil {
		log.Error("error creating HTTP listener", "err", err)
		shutdown.Fatal(err)
	}
	host.listener = l
	shutdown.BeforeExit(func() { host.Close() })

	// if we have a control socket FD, wait for a "resume" message before
	// opening state DBs and serving requests.
	var controlFD int
	if fdEnv := os.Getenv("FLYNN_CONTROL_FD"); fdEnv != "" {
		log.Info("parsing control socket file descriptor")
		controlFD, err = strconv.Atoi(fdEnv)
		if err != nil {
			log.Error("error parsing control socket file descriptor", "err", err)
			shutdown.Fatal(err)
		}

		log.Info("waiting for resume message from parent")
		msg := make([]byte, len(ControlMsgResume))
		if _, err := syscall.Read(controlFD, msg); err != nil {
			log.Error("error waiting for resume message from parent", "err", err)
			shutdown.Fatal(err)
		}

		log.Info("validating resume message")
		if !bytes.Equal(msg, ControlMsgResume) {
			log.Error(fmt.Sprintf("unexpected resume message from parent: %v", msg))
			shutdown.ExitWithCode(1)
		}

		log.Info("receiving log buffers from parent")
		if err := json.NewDecoder(&controlSock{controlFD}).Decode(&buffers); err != nil {
			log.Error("error receiving log buffers from parent", "err", err)
			shutdown.Fatal(err)
		}
	}

	log.Info("opening state databases")
	if err := host.OpenDBs(); err != nil {
		log.Error("error opening state databases", "err", err)
		shutdown.Fatal(err)
	}

	// stopJobs stops all jobs, leaving discoverd until the end so other
	// jobs can unregister themselves on shutdown.
	stopJobs := func() (err error) {
		var except []string
		host.statusMtx.RLock()
		if host.status.Discoverd != nil && host.status.Discoverd.JobID != "" {
			except = []string{host.status.Discoverd.JobID}
		}
		host.statusMtx.RUnlock()
		log.Info("stopping all jobs except discoverd")
		if err := backend.Cleanup(except); err != nil {
			log.Error("error stopping all jobs except discoverd", "err", err)
			return err
		}
		for _, id := range except {
			log.Info("stopping discoverd")
			if e := backend.Stop(id); e != nil {
				log.Error("error stopping discoverd", "err", err)
				err = e
			}
		}
		return
	}

	log.Info("restoring state")
	resurrect, err := state.Restore(backend, buffers)
	if err != nil {
		log.Error("error restoring state", "err", err)
		shutdown.Fatal(err)
	}
	shutdown.BeforeExit(func() {
		// close discoverd before stopping jobs so we can unregister first
		log.Info("unregistering with service discovery")
		if err := discoverdManager.Close(); err != nil {
			log.Error("error unregistering with service discovery", "err", err)
		}
		stopJobs()
	})
	shutdown.BeforeExit(func() {
		log.Info("marking jobs for resurrection")
		if err := state.MarkForResurrection(); err != nil {
			log.Error("error marking jobs for resurrection", "err", err)
		}
	})

	// configure network and discoverd if config set in host status
	if config := host.status.Network; config != nil {
		log.Info("configuring network", "subnet", config.Subnet, "mtu", config.MTU, "resolvers", config.Resolvers)
		if err := backend.ConfigureNetworking(config); err != nil {
			log.Error("error configuring network", "err", err)
			shutdown.Fatal(err)
		}
	}
	if config := host.status.Discoverd; config != nil && config.URL != "" {
		log.Info("connecting to service discovery", "url", config.URL)
		if err := host.connectDiscoverd(config.URL); err != nil {
			log.Error("error connecting to service discovery", "err", err)
			shutdown.Fatal(err)
		}
	}

	log.Info("serving HTTP requests")
	host.ServeHTTP()

	if controlFD > 0 {
		// now that we are serving requests, send an "ok" message to the parent
		log.Info("sending ok message to parent")
		if _, err := syscall.Write(controlFD, ControlMsgOK); err != nil {
			log.Error("error sending ok message to parent", "err", err)
			shutdown.Fatal(err)
		}

		log.Info("closing control socket")
		if err := syscall.Close(controlFD); err != nil {
			log.Error("error closing control socket", "err", err)
		}
	}

	if force {
		log.Info("forcibly stopping existing jobs")
		if err := stopJobs(); err != nil {
			log.Error("error forcibly stopping existing jobs", "err", err)
			shutdown.Fatal(err)
		}
	}

	if discoveryToken != "" {
		log.Info("getting cluster peer IPs")
		instances, err := discovery.GetCluster(discoveryToken)
		if err != nil {
			// TODO(titanous): retry?
			log.Error("error getting discovery cluster", "err", err)
			shutdown.Fatal(err)
		}
		peerIPs = make([]string, 0, len(instances))
		for _, inst := range instances {
			u, err := url.Parse(inst.URL)
			if err != nil {
				continue
			}
			ip, _, err := net.SplitHostPort(u.Host)
			if err != nil || ip == externalIP {
				continue
			}
			peerIPs = append(peerIPs, ip)
		}
		log.Info("got cluster peer IPs", "peers", peerIPs)
	}
	log.Info("connecting to cluster peers")
	if err := discoverdManager.ConnectPeer(peerIPs); err != nil {
		log.Info("no cluster peers available, resurrecting jobs")
		resurrect()
	}

	log.Info("blocking main goroutine")
	<-make(chan struct{})
}
