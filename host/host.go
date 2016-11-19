package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"

	"github.com/flynn/flynn/bootstrap/discovery"
	"github.com/flynn/flynn/host/cli"
	"github.com/flynn/flynn/host/config"
	"github.com/flynn/flynn/host/logmux"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/host/volume"
	"github.com/flynn/flynn/host/volume/api"
	"github.com/flynn/flynn/host/volume/manager"
	zfsVolume "github.com/flynn/flynn/host/volume/zfs"
	"github.com/flynn/flynn/pkg/shutdown"
	"github.com/flynn/flynn/pkg/version"
	"github.com/flynn/go-docopt"
	"github.com/opencontainers/runc/libcontainer"
	_ "github.com/opencontainers/runc/libcontainer/nsenter"
	"gopkg.in/inconshreveable/log15.v2"
)

const configFile = "/etc/flynn/host.json"

func init() {
	cli.Register("daemon", runDaemon, `
usage: flynn-host daemon [options]

options:
  --http-port=PORT           HTTP port [default: 1113]
  --external-ip=IP           external IP of host
  --listen-ip=IP             bind host network services to this IP
  --state=PATH               path to state file [default: /var/lib/flynn/host-state.bolt]
  --id=ID                    host id
  --tags=TAGS                host tags (comma separated list of KEY=VAL pairs, used for job constraints in the scheduler)
  --force                    kill all containers booted by flynn-host before starting
  --volpath=PATH             directory to create volumes in [default: /var/lib/flynn/volumes]
  --vol-provider=VOL         volume provider [default: zfs]
  --backend=BACKEND          runner backend [default: libcontainer]
  --flynn-init=PATH          path to flynn-init binary [default: /usr/local/bin/flynn-init]
  --log-dir=DIR              directory to store job logs [default: /var/log/flynn]
  --discovery=TOKEN          join cluster with discovery token
  --peer-ips=IPLIST          join existing cluster using IPs
  --bridge-name=NAME         network bridge name [default: flynnbr0]
  --no-resurrect             disable cluster resurrection
  --max-job-concurrency=NUM  maximum number of jobs to start concurrently
  --partitions=PARTITIONS    specify resource partitions for host [default: system=cpu_shares:4096 background=cpu_shares:4096 user=cpu_shares:8192]
  --init-log-level=LEVEL     containerinit log level [default: info]
  --zpool-name=NAME          zpool name
	`)
}

func main() {
	// when starting a container with libcontainer, we first exec the
	// current binary with libcontainer-init as the first argument,
	// which triggers the following code to initialise the container
	// environment (namespaces, network etc.) then exec containerinit
	if len(os.Args) > 1 && os.Args[1] == "libcontainer-init" {
		runtime.GOMAXPROCS(1)
		runtime.LockOSThread()
		factory, _ := libcontainer.New("")
		if err := factory.StartInitialization(); err != nil {
			log.Fatal(err)
		}
	}

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
  fix                        Fix a broken cluster
  tags                       Manage flynn-host daemon tags
  discover                   Return low-level information about a service
  promote                    Promotes a Flynn node to a member of the consensus cluster
  demote                     Demotes a Flynn node, removing it from the consensus cluster

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
		} else if _, ok := err.(cli.ErrAlreadyLogged); ok {
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
	tags := parseTagArgs(args.String["--tags"])
	force := args.Bool["--force"]
	volPath := args.String["--volpath"]
	volProvider := args.String["--vol-provider"]
	backendName := args.String["--backend"]
	flynnInit := args.String["--flynn-init"]
	logDir := args.String["--log-dir"]
	discoveryToken := args.String["--discovery"]
	bridgeName := args.String["--bridge-name"]

	logger, err := setupLogger(logDir)
	if err != nil {
		shutdown.Fatalf("error setting up logger: %s", err)
	}

	initLogLevel, err := log15.LvlFromString(args.String["--init-log-level"])
	if err != nil {
		shutdown.Fatalf("error setting init log level: %s", err)
	}

	var peerIPs []string
	if args.String["--peer-ips"] != "" {
		peerIPs = strings.Split(args.String["--peer-ips"], ",")
	}

	if hostID == "" {
		hostID = strings.Replace(hostname, "-", "", -1)
	}

	var maxJobConcurrency uint64 = 4
	if m, err := strconv.ParseUint(args.String["--max-job-concurrency"], 10, 64); err == nil {
		maxJobConcurrency = m
	}

	zpoolName := args.String["--zpool-name"]
	if zpoolName == "" {
		zpoolName = zfsVolume.DefaultDatasetName
	}

	if path, err := filepath.Abs(flynnInit); err == nil {
		flynnInit = path
	}

	var partitionCGroups = make(map[string]int64) // name -> cpu shares
	for _, p := range strings.Split(args.String["--partitions"], " ") {
		nameShares := strings.Split(p, "=cpu_shares:")
		if len(nameShares) != 2 {
			shutdown.Fatalf("invalid partition specifier: %q", p)
		}
		shares, err := strconv.ParseInt(nameShares[1], 10, 64)
		if err != nil || shares < 2 {
			shutdown.Fatalf("invalid cpu shares specifier: %q", shares)
		}
		partitionCGroups[nameShares[0]] = shares
	}
	for _, s := range []string{"user", "system", "background"} {
		if _, ok := partitionCGroups[s]; !ok {
			shutdown.Fatalf("missing mandatory resource partition: %s", s)
		}
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
			return zfsVolume.NewProvider(&zfsVolume.ProviderConfig{
				DatasetName: zpoolName,
				Make:        zfsVolume.DefaultMakeDev(volPath, log),
				WorkingDir:  filepath.Join(volPath, "zfs"),
			})
		}
	case "mock":
		newVolProvider = func() (volume.Provider, error) { return nil, nil }
	default:
		shutdown.Fatalf("unknown volume provider: %q", volProvider)
	}
	vman := volumemanager.New(
		filepath.Join(volPath, "volumes.bolt"),
		logger.New("component", "volumemanager"),
		newVolProvider,
	)
	shutdown.BeforeExit(func() { vman.CloseDB() })

	mux := logmux.New(hostID, logDir, logger.New("host.id", hostID, "component", "logmux"))

	log.Info("initializing job backend", "type", backendName)
	var backend Backend
	switch backendName {
	case "libcontainer":
		backend, err = NewLibcontainerBackend(&LibcontainerConfig{
			State:            state,
			VolManager:       vman,
			BridgeName:       bridgeName,
			InitPath:         flynnInit,
			InitLogLevel:     initLogLevel,
			LogMux:           mux,
			PartitionCGroups: partitionCGroups,
			Logger:           logger.New("host.id", hostID, "component", "backend", "backend", "libcontainer"),
		})
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
	discoverdManager := NewDiscoverdManager(backend, mux, hostID, publishAddr, tags)
	publishURL := "http://" + publishAddr
	host := &Host{
		id:  hostID,
		url: publishURL,
		status: &host.HostStatus{
			ID:      hostID,
			PID:     os.Getpid(),
			URL:     publishURL,
			Tags:    tags,
			Version: version.String(),
		},
		state:   state,
		backend: backend,
		vman:    vman,
		volAPI:  volumeapi.NewHTTPAPI(vman),
		discMan: discoverdManager,
		log:     logger.New("host.id", hostID),

		maxJobConcurrency: maxJobConcurrency,
	}
	backend.SetHost(host)

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
		// keep the same tags as the parent
		discoverdManager.UpdateTags(host.status.Tags)
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
		log.Info("no cluster peers available")
	}

	if !args.Bool["--no-resurrect"] {
		log.Info("resurrecting jobs")
		resurrect()
	}

	monitor := NewMonitor(host.discMan, externalIP, logger)
	shutdown.BeforeExit(func() { monitor.Shutdown() })
	go monitor.Run()

	log.Info("blocking main goroutine")
	<-make(chan struct{})
}

func parseTagArgs(args string) map[string]string {
	tags := make(map[string]string)
	for _, s := range strings.Split(args, ",") {
		keyVal := strings.SplitN(s, "=", 2)
		if len(keyVal) == 1 && keyVal[0] != "" {
			tags[keyVal[0]] = "true"
		} else if len(keyVal) == 2 {
			tags[keyVal[0]] = keyVal[1]
		}
	}
	return tags
}

func setupLogger(logDir string) (log15.Logger, error) {
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, err
	}
	path := filepath.Join(logDir, "flynn-host.log")
	handler, err := log15.FileHandler(path, log15.LogfmtFormat())
	if err != nil {
		return nil, err
	}
	log15.Root().SetHandler(handler)
	return log15.New("app", "host", "pid", os.Getpid()), nil
}
