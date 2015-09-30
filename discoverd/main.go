package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/vanillahsu/go_reuseport"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/discoverd/server"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/pkg/cluster"
	"github.com/flynn/flynn/pkg/shutdown"
)

const (
	// LeaderTimeout is the amount of time discoverd will wait for a leader.
	LeaderTimeout = 30 * time.Second
)

func main() {
	defer shutdown.Exit()

	// Initialize main program and execute.
	m := NewMain()
	if err := m.Run(os.Args[1:]...); err != nil {
		fmt.Fprintln(m.Stderr, err.Error())
		os.Exit(1)
	}

	// Wait indefinitely.
	<-(chan struct{})(nil)
}

// Main represents the main program execution.
type Main struct {
	mu           sync.Mutex
	status       host.DiscoverdConfig
	store        *server.Store
	dnsServer    *server.DNSServer
	httpListener net.Listener

	logger *log.Logger

	Stdout io.Writer
	Stderr io.Writer
}

// NewMain returns a new instance of Main.
func NewMain() *Main {
	return &Main{
		status: host.DiscoverdConfig{JobID: os.Getenv("FLYNN_JOB_ID")},

		Stdout: os.Stdout,
		Stderr: os.Stderr,
	}
}

// Run executes the program.
func (m *Main) Run(args ...string) error {
	// Create logger.
	m.logger = log.New(m.Stdout, "", log.LstdFlags)

	// Parse command line flags.
	opt, err := m.ParseFlags(args...)
	if err != nil {
		return err
	}

	// Set up advertised address and default peer set.
	advertiseAddr := MergeHostPort(opt.Host, opt.RaftAddr)
	m.logger.Printf("discoverd using advertise address %s", advertiseAddr)
	if len(opt.Peers) == 0 {
		opt.Peers, err = m.discoverPeers()
		if err != nil {
			return err
		}
	}
	if len(opt.Peers) == 0 {
		opt.Peers = []string{advertiseAddr}
	}

	// Open store if we are not proxying.
	m.logger.Printf("discoverd opening store with peers %s", opt.Peers)
	if err := m.openStore(opt.DataDir, opt.RaftAddr, opt.Host, advertiseAddr, opt.Peers); err != nil {
		return fmt.Errorf("Failed to open store: %s", err)
	}

	// Notify user that we're proxying if the store wasn't initialized.
	var raftHB discoverd.Heartbeater
	removed := make(chan struct{})
	if m.store == nil {
		m.logger.Print("advertised host not in peer set, joining as proxy")
	} else {
		ready := make(chan struct{})
		var once sync.Once
		if url := os.Getenv("DISCOVERD"); url != "" && url != "none" {
			client := discoverd.NewClientWithURL(url)
			raftHB, err = client.AddServiceAndRegister("discoverd-raft", advertiseAddr)
			if err != nil {
				return err
			}
		} else {
			once.Do(func() { close(ready) })
		}
		go func() {
			events := make(chan *discoverd.Event, 1000)
			go m.store.Subscribe("discoverd-raft", true, discoverd.EventKindAll, events)
			for event := range events {
				switch event.Kind {
				case discoverd.EventKindUp:
					addr := event.Instance.Addr
					m.logger.Printf("discoverd adding peer %s", addr)
					m.store.AddPeer(addr)
					if addr == advertiseAddr {
						once.Do(func() { close(ready) })
					}
				case discoverd.EventKindDown:
					addr := event.Instance.Addr
					m.logger.Printf("discoverd removing peer %s", addr)
					m.store.RemovePeer(addr)
					if addr == advertiseAddr {
						close(removed)
					}
				}
			}
		}()
		select {
		case <-ready:
		case <-time.After(time.Minute):
			return errors.New("timed out waiting to join cluster")
		}
	}

	// Create a slice of peers with their HTTP address set instead.
	httpPeers, err := SetPortSlice(opt.Peers, opt.HTTPAddr)
	if err != nil {
		return fmt.Errorf("set port slice: %s", err)
	}

	// If we have a DNS address, start a DNS server right away, otherwise
	// wait for the host network to come up and then start a DNS server.
	if opt.DNSAddr != "" {
		if err := m.openDNSServer(opt.DNSAddr, opt.Recursors, httpPeers); err != nil {
			return fmt.Errorf("Failed to start DNS server: %s", err)
		}
		m.logger.Printf("discoverd listening for DNS on %s", opt.DNSAddr)
	} else if opt.WaitNetDNS {
		go func() {
			// Wait for the host network.
			status, err := cluster.WaitForHostStatus(os.Getenv("EXTERNAL_IP"), func(status *host.HostStatus) bool {
				return status.Network != nil && status.Network.Subnet != ""
			})
			if err != nil {
				log.Fatal(err)
			}

			// Parse network subnet to determine bind address.
			ip, _, err := net.ParseCIDR(status.Network.Subnet)
			if err != nil {
				log.Fatal(err)
			}
			addr := net.JoinHostPort(ip.String(), "53")

			if err := m.openDNSServer(addr, status.Network.Resolvers, httpPeers); err != nil {
				log.Fatalf("Failed to start DNS server: %s", err)
			}
			m.logger.Printf("discoverd listening for DNS on %s", addr)

			// Notify webhook.
			if opt.Notify != "" {
				m.Notify(opt.Notify, "", addr)
			}
		}()
	}

	if err := m.openHTTPServer(opt.HTTPAddr, opt.Peers); err != nil {
		return fmt.Errorf("Failed to start HTTP server: %s", err)
	}

	// Notify user that the servers are listening.
	m.logger.Printf("discoverd listening for HTTP on %s", opt.HTTPAddr)

	// Wait for leadership.
	if err := m.waitForLeader(LeaderTimeout); err != nil {
		return err
	}

	// Notify URL that discoverd is running.
	httpAddr := m.httpListener.Addr().String()
	host, port, _ := net.SplitHostPort(httpAddr)
	if host == "0.0.0.0" {
		httpAddr = net.JoinHostPort(os.Getenv("EXTERNAL_IP"), port)
	}
	m.Notify(opt.Notify, "http://"+httpAddr, opt.DNSAddr)

	// Register services
	client := discoverd.NewClientWithURL("http://" + httpAddr)
	if raftHB == nil {
		raftHB, err = client.AddServiceAndRegister("discoverd-raft", advertiseAddr)
		if err != nil {
			return fmt.Errorf("error registering service %q: %s", "discoverd-raft", err)
		}
	} else {
		raftHB.SetClient(client)
	}
	shutdown.BeforeExit(func() {
		raftHB.Close()
		<-removed
		m.store.Close()
	})

	hb, err := client.AddServiceAndRegister("discoverd", httpAddr)
	if err != nil {
		return fmt.Errorf("error registering service %q: %s", "discoverd", err)
	}
	shutdown.BeforeExit(func() { hb.Close() })

	return nil
}

// Close shuts down all open servers.
func (m *Main) Close() error {
	if m.store != nil {
		m.store.Close()
		m.store = nil
	}
	if m.dnsServer != nil {
		m.dnsServer.Close()
		m.dnsServer = nil
	}
	if m.httpListener != nil {
		m.httpListener.Close()
		m.httpListener = nil
	}
	return nil
}

func (m *Main) discoverPeers() ([]string, error) {
	url := os.Getenv("DISCOVERD")
	if url == "" || url == "none" {
		return nil, nil
	}
	client := discoverd.NewClientWithURL(url)
	instances, err := client.Instances("discoverd-raft", 10*time.Second)
	if err != nil {
		return nil, err
	}
	peers := make([]string, len(instances))
	for i, inst := range instances {
		peers[i] = inst.Addr
	}
	return peers, nil
}

// openStore initializes and opens the store.
func (m *Main) openStore(path, bindAddress, host, advertise string, peers []string) error {
	// If the host is not the same as any of the peers then we should proxy.
	proxying := true

	for _, addr := range peers {
		if h, _, _ := net.SplitHostPort(addr); h == host {
			proxying = false
		}
	}
	if proxying {
		return nil
	}

	// Resolve advertised address.
	addr, err := net.ResolveTCPAddr("tcp", advertise)
	if err != nil {
		return err
	}

	// Initialize store.
	s := server.NewStore(path)
	s.BindAddress = bindAddress
	s.Advertise = addr

	// Allow single node if there's no peers set.
	s.EnableSingleNode = len(peers) <= 1

	// Open store.
	if err := s.Open(); err != nil {
		return err
	}
	m.store = s

	// If peers then set peer set.
	if len(peers) > 0 {
		if err := s.SetPeers(peers); err != nil {
			return fmt.Errorf("set peers: %s", err)
		}
	}

	return nil
}

// openDNSServer initializes and opens the DNS server.
// The store must already be open.
func (m *Main) openDNSServer(addr string, recursors, peers []string) error {
	s := &server.DNSServer{
		UDPAddr:   addr,
		TCPAddr:   addr,
		Recursors: recursors,
	}

	// If store is available then attach it. Otherwise use a proxy.
	if m.store != nil {
		s.Store = m.store
	} else {
		s.Store = &server.ProxyStore{Peers: peers}
	}

	if err := s.ListenAndServe(); err != nil {
		return err
	}
	m.dnsServer = s
	return nil
}

// openHTTPServer initializes and opens the HTTP server.
// The store must already be open.
func (m *Main) openHTTPServer(addr string, peers []string) error {
	// Open HTTP API.
	ln, err := reuseport.NewReusablePortListener("tcp4", addr)
	if err != nil {
		return err
	}
	m.httpListener = ln

	// If we have no store then simply start a proxy handler.
	if m.store == nil {
		go http.Serve(m.httpListener, &server.ProxyHandler{Peers: peers})
		return nil
	}

	// Otherwise initialize and start handler.
	h := server.NewHandler()
	h.Store = m.store
	go http.Serve(m.httpListener, h)

	return nil
}

// Notify sends a POST to notifyURL to let it know that addr is accessible.
func (m *Main) Notify(notifyURL, httpURL, dnsAddr string) {
	m.mu.Lock()
	if httpURL != "" {
		m.status.URL = httpURL
	}
	if dnsAddr != "" {
		m.status.DNS = dnsAddr
	}
	payload, _ := json.Marshal(m.status)
	m.mu.Unlock()

	res, err := http.Post(notifyURL, "application/json", bytes.NewReader(payload))
	if err != nil {
		m.logger.Printf("failed to notify: %s", err)
	} else {
		res.Body.Close()
	}
}

// MergeHostPort joins host to the port in portAddr.
func MergeHostPort(host, portAddr string) string {
	_, port, _ := net.SplitHostPort(portAddr)
	return net.JoinHostPort(host, port)
}

// waitForLeader polls the store until a leader is found or a timeout occurs.
func (m *Main) waitForLeader(timeout time.Duration) error {
	// Ignore leadership if we are a proxy.
	if m.store == nil {
		return nil
	}

	timeoutCh := time.After(timeout)
	for {
		select {
		case <-timeoutCh:
			return errors.New("timed out waiting for leader")
		case <-time.After(100 * time.Millisecond):
			if leader := m.store.Leader(); leader != "" {
				return nil
			}
		}
	}
}

// ParseFlags parses the command line flags.
func (m *Main) ParseFlags(args ...string) (Options, error) {
	var opt Options
	var peers, recursors string

	fs := flag.NewFlagSet("discoverd", flag.ContinueOnError)
	fs.SetOutput(m.Stderr)
	fs.StringVar(&opt.DataDir, "data-dir", "", "data directory")
	fs.StringVar(&peers, "peers", "", "cluster peers")
	fs.StringVar(&opt.Host, "host", "", "advertised hostname")
	fs.StringVar(&opt.RaftAddr, "raft-addr", ":1110", "address to serve raft cluster from")
	fs.StringVar(&opt.HTTPAddr, "http-addr", ":1111", "address to serve HTTP API from")
	fs.StringVar(&opt.DNSAddr, "dns-addr", "", "address to service DNS from")
	fs.StringVar(&recursors, "recursors", "8.8.8.8,8.8.4.4", "upstream recursive DNS servers")
	fs.StringVar(&opt.Notify, "notify", "", "url to send webhook to after starting listener")
	fs.BoolVar(&opt.WaitNetDNS, "wait-net-dns", false, "start DNS server after host network is configured")
	if err := fs.Parse(args); err != nil {
		return Options{}, err
	}

	// Split peer hostnames into slice.
	if peers != "" {
		opt.Peers = TrimSpaceSlice(strings.Split(peers, ","))
	}

	// Split recursors into slice.
	if recursors != "" {
		opt.Recursors = TrimSpaceSlice(strings.Split(recursors, ","))
	}

	// Validate options.
	if opt.DataDir == "" {
		return opt, errors.New("data directory required")
	} else if opt.Host == "" {
		return opt, errors.New("host required")
	}

	return opt, nil
}

// Options represents the command line options.
type Options struct {
	DataDir    string   // data directory
	Host       string   // hostname
	Peers      []string // cluster peers
	RaftAddr   string   // raft bind address
	HTTPAddr   string   // http bind address
	DNSAddr    string   // dns bind address
	Recursors  []string // dns recursors
	Notify     string   // notify URL
	WaitNetDNS bool     // wait for the network DNS
}

// TrimSpaceSlice returns a new slice of trimmed strings.
// Empty strings are removed entirely.
func TrimSpaceSlice(a []string) []string {
	other := make([]string, 0, len(a))
	for _, s := range a {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		other = append(other, s)
	}
	return other
}

// SetPortSlice sets the ports for a slice of hosts.
func SetPortSlice(peers []string, addr string) ([]string, error) {
	// Retrieve the port from addr.
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}

	// Merge the port with the peer hosts into a new slice.
	other := make([]string, len(peers))
	for i, peer := range peers {
		host, _, err := net.SplitHostPort(peer)
		if err != nil {
			return nil, err
		}

		other[i] = net.JoinHostPort(host, port)
	}

	return other, nil
}
