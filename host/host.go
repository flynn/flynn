package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-docopt"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/technoweenie/grohl"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/host/cli"
	"github.com/flynn/flynn/host/config"
	"github.com/flynn/flynn/host/logmux"
	"github.com/flynn/flynn/host/volume"
	"github.com/flynn/flynn/host/volume/manager"
	zfsVolume "github.com/flynn/flynn/host/volume/zfs"
	"github.com/flynn/flynn/pkg/cluster"
	"github.com/flynn/flynn/pkg/shutdown"
	"github.com/flynn/flynn/pkg/version"
)

const configFile = "/etc/flynn/host.json"

func init() {
	log.SetFlags(log.Lshortfile | log.Lmicroseconds)

	cli.Register("daemon", runDaemon, `
usage: flynn-host daemon [options]

options:
  --external=IP          external IP of host
  --state=PATH           path to state file [default: /var/lib/flynn/host-state.bolt]
  --id=ID                host id
  --force                kill all containers booted by flynn-host before starting
  --legacy-volpath=PATH  directory to create legacy volumes in [default: /var/lib/flynn/host-volumes]
  --volpath=PATH         directory to create volumes in [default: /var/lib/flynn/volumes]
  --backend=BACKEND      runner backend [default: libvirt-lxc]
  --flynn-init=PATH      path to flynn-init binary [default: /usr/local/bin/flynn-init]
  --log-dir=DIR          directory to store job logs [default: /var/log/flynn]
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
  destroy-volumes            Destroys the local volume database
  collect-debug-info         Collect debug information into an anonymous gist or tarball
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
				log.Fatalf("error opening config file %s: %s", n, err)
			}
		} else {
			var err error
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
	externalAddr := args.String["--external"]
	stateFile := args.String["--state"]
	hostID := args.String["--id"]
	force := args.Bool["--force"]
	legacyVolPath := args.String["--legacyvolpath"]
	volPath := args.String["--volpath"]
	backendName := args.String["--backend"]
	flynnInit := args.String["--flynn-init"]
	logDir := args.String["--log-dir"]

	grohl.AddContext("app", "host")
	grohl.Log(grohl.Data{"at": "start"})

	if hostID == "" {
		hostID = strings.Replace(hostname, "-", "", -1)
	}
	if strings.Contains(hostID, "-") {
		shutdown.Fatal("host id must not contain dashes")
	}
	if externalAddr == "" {
		var err error
		externalAddr, err = config.DefaultExternalIP()
		if err != nil {
			shutdown.Fatal(err)
		}
	}

	state := NewState(hostID, stateFile)
	var backend Backend
	var err error

	// create volume manager
	vman, err := volumemanager.New(
		filepath.Join(volPath, "volumes.bolt"),
		func() (volume.Provider, error) {
			// use a zpool backing file size of either 70% of the device on which
			// volumes will reside, or 100GB if that can't be determined.
			var size int64
			var dev syscall.Statfs_t
			if err := syscall.Statfs(volPath, &dev); err == nil {
				size = (dev.Bsize * int64(dev.Blocks) * 7) / 10
			} else {
				size = 100000000000
			}
			g.Log(grohl.Data{"at": "zpool_size", "size": size})

			return zfsVolume.NewProvider(&zfsVolume.ProviderConfig{
				DatasetName: "flynn-default",
				Make: &zfsVolume.MakeDev{
					BackingFilename: filepath.Join(volPath, "zfs/vdev/flynn-default-zpool.vdev"),
					Size:            size,
				},
				WorkingDir: filepath.Join(volPath, "zfs"),
			})
		},
	)
	if err != nil {
		shutdown.Fatal(err)
	}

	mux := logmux.New(1000)
	shutdown.BeforeExit(func() { mux.Close() })

	switch backendName {
	case "libvirt-lxc":
		backend, err = NewLibvirtLXCBackend(state, vman, legacyVolPath, logDir, flynnInit, mux)
	default:
		log.Fatalf("unknown backend %q", backendName)
	}
	if err != nil {
		shutdown.Fatal(err)
	}

	if err := state.Restore(backend); err != nil {
		shutdown.Fatal(err)
	}

	shutdown.BeforeExit(func() { backend.Cleanup() })
	shutdown.BeforeExit(func() {
		if err := state.MarkForResurrection(); err != nil {
			log.Print("error marking for resurrection", err)
		}
	})

	if force {
		if err := backend.Cleanup(); err != nil {
			shutdown.Fatal(err)
		}
	}

	discoverdConfigured := false
	connectDiscoverd := func(url string) error {
		if discoverdConfigured {
			return errors.New("host: discoverd is already configured")
		}
		disc := discoverd.NewClientWithURL(url)
		// TODO: use attempts to connect
		hb, err := disc.AddServiceAndRegisterInstance("flynn-host", &discoverd.Instance{
			Addr: externalAddr + ":1113",
			Meta: map[string]string{"id": hostID},
		})
		if err != nil {
			return err
		}
		shutdown.BeforeExit(func() { hb.Close() })

		if err := mux.Connect(disc, "flynn-logaggregator"); err != nil {
			shutdown.Fatal(err)
		}
		discoverdConfigured = true
		backend.SetDefaultEnv("DISCOVERD", url)
	}

	var cluster *cluster.Client
	shutdown.Fatal(serveHTTP(
		&Host{state: state, backend: backend},
		&attachHandler{state: state, backend: backend},
		cluster,
		vman,
		connectDiscoverd,
	))
}
