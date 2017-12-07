package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	disc "github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/flannel/backend"
	"github.com/flynn/flynn/flannel/backend/alloc"
	"github.com/flynn/flynn/flannel/backend/hostgw"
	"github.com/flynn/flynn/flannel/backend/vxlan"
	"github.com/flynn/flynn/flannel/discoverd"
	"github.com/flynn/flynn/flannel/pkg/ip"
	"github.com/flynn/flynn/flannel/pkg/task"
	"github.com/flynn/flynn/flannel/subnet"
	"github.com/flynn/flynn/pkg/keepalive"
	"github.com/flynn/flynn/pkg/status"
	"github.com/flynn/flynn/pkg/version"
	log "github.com/golang/glog"
)

type CmdLineOpts struct {
	help         bool
	version      bool
	ipMasq       bool
	subnetFile   string
	iface        string
	notifyURL    string
	discoverdURL string
	httpPort     string
}

var opts CmdLineOpts

func init() {
	flag.StringVar(&opts.subnetFile, "subnet-file", "/run/flannel/subnet.env", "filename where env variables (subnet and MTU values) will be written to")
	flag.StringVar(&opts.notifyURL, "notify-url", "", "URL to send webhook after starting")
	flag.StringVar(&opts.discoverdURL, "discoverd-url", "", "URL of discoverd registry")
	flag.StringVar(&opts.iface, "iface", "", "interface to use (IP or name) for inter-host communication")
	flag.StringVar(&opts.httpPort, "http-port", "5001", "port to listen for HTTP requests on allocated IP")
	flag.BoolVar(&opts.ipMasq, "ip-masq", false, "setup IP masquerade rule for traffic destined outside of overlay network")
	flag.BoolVar(&opts.help, "help", false, "print this message")
	flag.BoolVar(&opts.version, "version", false, "print version and exit")
}

// flagsFromEnv parses all registered flags in the given flagset,
// and if they are not already set it attempts to set their values from
// environment variables. Environment variables take the name of the flag but
// are UPPERCASE, have the given prefix, and any dashes are replaced by
// underscores - for example: some-flag => PREFIX_SOME_FLAG
func flagsFromEnv(prefix string, fs *flag.FlagSet) {
	alreadySet := make(map[string]bool)
	fs.Visit(func(f *flag.Flag) {
		alreadySet[f.Name] = true
	})
	fs.VisitAll(func(f *flag.Flag) {
		if !alreadySet[f.Name] {
			key := strings.ToUpper(prefix + "_" + strings.Replace(f.Name, "-", "_", -1))
			val := os.Getenv(key)
			if val != "" {
				fs.Set(f.Name, val)
			}
		}
	})
}

func writeSubnetFile(sn *backend.SubnetDef) error {
	// Write out the first usable IP by incrementing
	// sn.IP by one
	net := sn.Net
	net.IP += 1

	dir, name := filepath.Split(opts.subnetFile)
	os.MkdirAll(dir, 0755)

	tempFile := filepath.Join(dir, "."+name)
	f, err := os.Create(tempFile)
	if err != nil {
		return err
	}

	fmt.Fprintf(f, "FLANNEL_SUBNET=%s\n", net)
	fmt.Fprintf(f, "FLANNEL_MTU=%d\n", sn.MTU)
	_, err = fmt.Fprintf(f, "FLANNEL_IPMASQ=%v\n", opts.ipMasq)
	f.Close()
	if err != nil {
		return err
	}

	// rename(2) the temporary file to the desired location so that it becomes
	// atomically visible with the contents
	return os.Rename(tempFile, opts.subnetFile)
}

func notifyWebhook(sn *backend.SubnetDef) error {
	if opts.notifyURL == "" {
		return nil
	}
	net := sn.Net
	net.IP += 1
	data := struct {
		JobID  string `json:"job_id"`
		Subnet string `json:"subnet"`
		MTU    int    `json:"mtu"`
	}{os.Getenv("FLYNN_JOB_ID"), net.String(), sn.MTU}
	payload, _ := json.Marshal(data)
	res, err := http.Post(opts.notifyURL, "application/json", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	res.Body.Close()
	return nil
}

func lookupIface() (*net.Interface, net.IP, error) {
	var iface *net.Interface
	var ipaddr net.IP
	var err error

	if len(opts.iface) > 0 {
		if ipaddr = net.ParseIP(opts.iface); ipaddr != nil {
			iface, err = ip.GetInterfaceByIP(ipaddr)
			if err != nil {
				return nil, nil, fmt.Errorf("Error looking up interface %s: %s", opts.iface, err)
			}
		} else {
			iface, err = net.InterfaceByName(opts.iface)
			if err != nil {
				return nil, nil, fmt.Errorf("Error looking up interface %s: %s", opts.iface, err)
			}
		}
	} else {
		log.Info("Determining IP address of default interface")
		if iface, err = ip.GetDefaultGatewayIface(); err != nil {
			return nil, nil, fmt.Errorf("Failed to get default interface: %s", err)
		}
	}

	if ipaddr == nil {
		ipaddr, err = ip.GetIfaceIP4Addr(iface)
		if err != nil {
			return nil, nil, fmt.Errorf("Failed to find IPv4 address for interface %s", iface.Name)
		}
	}

	return iface, ipaddr, nil
}

func makeSubnetManager() *subnet.SubnetManager {
	var registryFn func() (subnet.Registry, error)
	client := disc.NewClientWithURL(opts.discoverdURL)
	registryFn = func() (subnet.Registry, error) {
		return discoverd.NewRegistry(client, "flannel")
	}

	for {
		reg, err := registryFn()
		if err != nil {
			log.Error("Failed to create subnet registry: ", err)
			time.Sleep(time.Second)
			continue
		}

		sm, err := subnet.NewSubnetManager(reg)
		if err == nil {
			return sm
		}

		log.Error("Failed to create SubnetManager: ", err)
		time.Sleep(time.Second)
	}
}

func newBackend() (backend.Backend, *subnet.SubnetManager, error) {
	sm := makeSubnetManager()
	config := sm.GetConfig()

	var bt struct {
		Type string
	}

	if len(config.Backend) == 0 {
		bt.Type = "vxlan"
	} else {
		if err := json.Unmarshal(config.Backend, &bt); err != nil {
			return nil, nil, fmt.Errorf("Error decoding Backend property of config: %v", err)
		}
	}

	switch strings.ToLower(bt.Type) {
	case "alloc":
		return alloc.New(sm), sm, nil
	case "host-gw":
		return hostgw.New(sm), sm, nil
	case "vxlan":
		return vxlan.New(sm, config.Backend), sm, nil
	default:
		return nil, nil, fmt.Errorf("'%v': unknown backend type", bt.Type)
	}
}

func httpServer(sn *subnet.SubnetManager, publicIP, port string) error {
	overlayListener, err := net.Listen("tcp", net.JoinHostPort(sn.Lease().Network.IP.String(), port))
	if err != nil {
		return err
	}
	publicListener, err := net.Listen("tcp", net.JoinHostPort(publicIP, port))
	if err != nil {
		return err
	}

	http.HandleFunc("/ping", func(http.ResponseWriter, *http.Request) {})
	status.AddHandler(status.SimpleHandler(func() error {
		return pingLeases(sn.Leases())
	}))
	go http.Serve(keepalive.Listener(overlayListener), nil)
	go http.Serve(keepalive.Listener(publicListener), nil)
	return nil
}

// ping neighbor leases five at a time, timeout 1 second, returning as soon as
// one returns success.
func pingLeases(leases []subnet.SubnetLease) error {
	const workers = 5
	const timeout = 1 * time.Second

	if len(leases) == 0 {
		return nil
	}

	work := make(chan subnet.SubnetLease)
	results := make(chan bool, workers)
	client := http.Client{Timeout: timeout}

	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			for l := range work {
				res, err := client.Get(fmt.Sprintf("http://%s:%s/ping", l.Network.IP, l.Attrs.HTTPPort))
				if err == nil {
					res.Body.Close()
				}
				results <- err == nil && res.StatusCode == 200
			}
			wg.Done()
		}()
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	for _, l := range leases {
		select {
		case work <- l:
		case success := <-results:
			if success {
				close(work)
				return nil
			}
		}
	}
	close(work)

	for success := range results {
		if success {
			return nil
		}
	}

	return errors.New("failed to successfully ping a neighbor")
}

func run(be backend.Backend, sm *subnet.SubnetManager, exit chan int) {
	var err error
	defer func() {
		if err == nil || err == task.ErrCanceled {
			exit <- 0
		} else {
			log.Error(err)
			exit <- 1
		}
	}()

	iface, ipaddr, err := lookupIface()
	if err != nil {
		return
	}

	if iface.MTU == 0 {
		err = fmt.Errorf("Failed to determine MTU for %s interface", ipaddr)
		return
	}

	log.Infof("Using %s as external interface", ipaddr)

	sn, err := be.Init(iface, ipaddr, opts.httpPort, opts.ipMasq)
	if err != nil {
		return
	}

	writeSubnetFile(sn)
	notifyWebhook(sn)

	if err = httpServer(sm, ipaddr.String(), opts.httpPort); err != nil {
		err = fmt.Errorf("error starting HTTP server: %s", err)
		return
	}
	if opts.discoverdURL != "" {
		disc.NewClientWithURL(opts.discoverdURL).AddServiceAndRegister("flannel", net.JoinHostPort(ipaddr.String(), opts.httpPort))
	}

	log.Infof("%s mode initialized", be.Name())
	be.Run()
}

func main() {
	// glog will log to tmp files by default. override so all entries
	// can flow into journald (if running under systemd)
	flag.Set("logtostderr", "true")

	// now parse command line args
	flag.Parse()

	if opts.help {
		fmt.Fprintf(os.Stderr, "Usage: %s [OPTION]...\n", os.Args[0])
		flag.PrintDefaults()
		os.Exit(0)
	}

	if opts.version {
		fmt.Fprintln(os.Stderr, version.String())
		os.Exit(0)
	}

	flagsFromEnv("FLANNELD", flag.CommandLine)

	be, sm, err := newBackend()
	if err != nil {
		log.Info(err)
		os.Exit(1)
	}

	// Register for SIGINT and SIGTERM and wait for one of them to arrive
	log.Info("Installing signal handlers")
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)

	exit := make(chan int)
	go run(be, sm, exit)

	for {
		select {
		case <-sigs:
			// unregister to get default OS nuke behaviour in case we don't exit cleanly
			signal.Stop(sigs)

			log.Info("Exiting...")
			be.Stop()

		case code := <-exit:
			log.Infof("%s mode exited", be.Name())
			os.Exit(code)
		}
	}
}
