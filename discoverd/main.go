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

	"github.com/flynn/flynn/discoverd/client"
	dd "github.com/flynn/flynn/discoverd/deployment"
	"github.com/flynn/flynn/discoverd/server"
	dt "github.com/flynn/flynn/discoverd/types"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/pkg/cluster"
	"github.com/flynn/flynn/pkg/keepalive"
	"github.com/flynn/flynn/pkg/mux"
	"github.com/flynn/flynn/pkg/shutdown"
)

const (
	// Wait indefinitely
	IndefiniteTimeout = time.Duration(-1)
)

func main() {
	defer shutdown.Exit()

	// Initialize main program and execute.
	m := NewMain()
	shutdown.BeforeExit(func() { fmt.Fprintln(m.Stderr, "discoverd is exiting") })
	if err := m.Run(os.Args[1:]...); err != nil {
		fmt.Fprintln(m.Stderr, err.Error())
		os.Exit(1)
	}

	// Wait indefinitely.
	<-(chan struct{})(nil)
}

// Main represents the main program execution.
type Main struct {
	mu         sync.Mutex
	status     host.DiscoverdConfig
	store      *server.Store
	dnsServer  *server.DNSServer
	httpServer *http.Server
	ln         net.Listener
	hb         discoverd.Heartbeater
	mux        *mux.Mux

	advertiseAddr string
	dataDir       string
	handler       *server.Handler
	peers         []string

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
	m.advertiseAddr = MergeHostPort(opt.Host, opt.Addr)
	if len(opt.Peers) == 0 {
		opt.Peers = []string{m.advertiseAddr}
	}

	// Create a slice of peers with their HTTP address set instead.
	httpPeers, err := SetPortSlice(opt.Peers, opt.Addr)
	if err != nil {
		return fmt.Errorf("set port slice: %s", err)
	}
	m.peers = httpPeers

	// Initialise the default client using the peer list
	os.Setenv("DISCOVERD", strings.Join(opt.Peers, ","))
	discoverd.DefaultClient = discoverd.NewClient()

	// if there is a discoverd process already running on this
	// address perform a deployment by starting a proxy DNS server
	// and shutting down the old discoverd job
	var deploy *dd.Deployment
	var targetLogIndex dt.TargetLogIndex

	target := fmt.Sprintf("http://%s:1111", opt.Host)
	m.logger.Println("checking for existing discoverd process at", target)
	if err := discoverd.NewClientWithURL(target).Ping(target); err == nil {
		m.logger.Println("discoverd responding at", target, "taking over")

		deploy, err = dd.NewDeployment("discoverd")
		if err != nil {
			return err
		}
		m.logger.Println("Created deployment")
		if err := deploy.MarkPerforming(m.advertiseAddr, 60); err != nil {
			return err
		}
		m.logger.Println("marked", m.advertiseAddr, "as performing in deployent")
		addr, resolvers := waitHostDNSConfig()
		if opt.DNSAddr != "" {
			addr = opt.DNSAddr
		}
		if len(opt.Recursors) > 0 {
			resolvers = opt.Recursors
		}
		m.logger.Println("starting proxy DNS server")
		if err := m.openDNSServer(addr, resolvers); err != nil {
			return fmt.Errorf("Failed to start DNS server: %s", err)
		}
		m.logger.Printf("discoverd listening for DNS on %s", addr)

		targetLogIndex, err = discoverd.NewClientWithURL(target).Shutdown(target)
		if err != nil {
			return err
		}
		// Sleep for 2x the election timeout.
		// This is to work around an issue with hashicorp/raft that can allow us to be elected with
		// no log entries, hence truncating the log and losing all data!
		time.Sleep(2 * time.Second)
	} else {
		m.logger.Println("failed to contact existing discoverd server, starting up without takeover")
		m.logger.Println("err:", err)
	}

	// Open listener.
	ln, err := net.Listen("tcp4", opt.Addr)
	if err != nil {
		return err
	}
	m.ln = keepalive.Listener(ln)

	// Open mux
	m.mux = mux.New(m.ln)
	go m.mux.Serve()

	m.dataDir = opt.DataDir

	// if the advertise addr is not in the peer list we are proxying
	proxying := true
	for _, addr := range m.peers {
		if addr == m.advertiseAddr {
			proxying = false
			break
		}
	}

	if proxying {
		// Notify user that we're proxying if the store wasn't initialized.
		m.logger.Println("advertised address not in peer set, joining as proxy")
	} else {
		// Open store if we are not proxying.
		if err := m.openStore(); err != nil {
			return fmt.Errorf("Failed to open store: %s", err)
		}
	}

	// Wait for the store to catchup before switching to local store if we are doing a deployment
	if m.store != nil && targetLogIndex.LastIndex > 0 {
		for m.store.LastIndex() < targetLogIndex.LastIndex {
			m.logger.Println("Waiting for store to catchup, current:", m.store.LastIndex(), "target:", targetLogIndex.LastIndex)
			time.Sleep(100 * time.Millisecond)
		}
	}

	// If we already started the DNS server as part of a deployment above,
	// and we have an initialized store, just switch from the proxy store
	// to the initialized store.
	//
	// Else if we have a DNS address, start a DNS server right away.
	//
	// Otherwise wait for the host network to come up and then start a DNS
	// server.
	if m.dnsServer != nil && m.store != nil {
		m.dnsServer.SetStore(m.store)
	} else if opt.DNSAddr != "" {
		if err := m.openDNSServer(opt.DNSAddr, opt.Recursors); err != nil {
			return fmt.Errorf("Failed to start DNS server: %s", err)
		}
		m.logger.Printf("discoverd listening for DNS on %s", opt.DNSAddr)
	} else if opt.WaitNetDNS {
		go func() {
			addr, resolvers := waitHostDNSConfig()
			m.mu.Lock()
			if err := m.openDNSServer(addr, resolvers); err != nil {
				log.Fatalf("Failed to start DNS server: %s", err)
			}
			m.mu.Unlock()
			m.logger.Printf("discoverd listening for DNS on %s", addr)

			// Notify webhook.
			if opt.Notify != "" {
				m.Notify(opt.Notify, addr)
			}
		}()
	}

	if err := m.openHTTPServer(); err != nil {
		return fmt.Errorf("Failed to start HTTP server: %s", err)
	}

	if deploy != nil {
		if err := deploy.MarkDone(m.advertiseAddr); err != nil {
			return err
		}
		m.logger.Println("marked", m.advertiseAddr, "as done in deployment")
	}

	// Notify user that the servers are listening.
	m.logger.Printf("discoverd listening for HTTP on %s", opt.Addr)

	// Wait for leadership.
	if err := m.waitForLeader(IndefiniteTimeout); err != nil {
		return err
	}

	// Notify URL that discoverd is running.
	httpAddr := ln.Addr().String()
	host, port, _ := net.SplitHostPort(httpAddr)
	if host == "0.0.0.0" {
		httpAddr = net.JoinHostPort(os.Getenv("EXTERNAL_IP"), port)
	}
	m.Notify(opt.Notify, opt.DNSAddr)
	go func() {
		for {
			hb, err := discoverd.AddServiceAndRegister("discoverd", httpAddr)
			if err != nil {
				m.logger.Println("failed to register service/instance, retrying in 5 seconds:", err)
				time.Sleep(5 * time.Second)
				continue
			}
			m.mu.Lock()
			m.hb = hb
			m.mu.Unlock()
			break
		}
	}()
	return nil
}

func waitHostDNSConfig() (addr string, resolvers []string) {
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
	addr = net.JoinHostPort(ip.String(), "53")
	return addr, status.Network.Resolvers
}

// Join the consensus set, promoting ourselves from proxy to raft node.
func (m *Main) Promote() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.logger.Printf("attempting promotion")

	// Request the leader joins us to the cluster.
	m.logger.Println("requesting leader join us to cluster")
	targetLogIndex, err := discoverd.DefaultClient.RaftAddPeer(m.advertiseAddr)
	if err != nil {
		m.logger.Println("error requesting leader to join us to cluster:", err)
		return err
	}

	// Open the store.
	if m.store == nil {
		if err := m.openStore(); err != nil {
			return err
		}

		// Wait for leadership.
		if err := m.waitForLeader(60 * time.Second); err != nil {
			return err
		}

		// Wait for store to catchup
		for m.store.LastIndex() < targetLogIndex.LastIndex {
			m.logger.Println("Waiting for store to catchup, current:", m.store.LastIndex(), "target:", targetLogIndex.LastIndex)
			time.Sleep(100 * time.Millisecond)
		}
	}

	// Update the DNS server to use the local store.
	if m.dnsServer != nil {
		m.dnsServer.SetStore(m.store)
	}

	// Update the HTTP server to use the local store.
	m.handler.Store = m.store
	m.handler.Proxy.Store(false)

	m.logger.Println("promoted successfully")
	return nil
}

// Leave the consensus set, demoting ourselves to proxy from raft node.
func (m *Main) Demote() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.logger.Println("demotion requested")

	var leaderAddr string
	if m.store != nil {
		leaderAddr := m.store.Leader()
		if leaderAddr == "" {
			return server.ErrNoKnownLeader
		}
	} else {
		leader, err := discoverd.DefaultClient.RaftLeader()
		if err != nil || (err == nil && leader.Host == "") {
			return fmt.Errorf("failed to get leader address from configured peers")
		}
		leaderAddr = leader.Host
	}

	// Update the dns server to use proxy store
	if m.dnsServer != nil {
		m.dnsServer.SetStore(&server.ProxyStore{Peers: m.peers})
	}

	// Set handler into proxy mode
	m.handler.Proxy.Store(true)

	// If we fail from here on out we should attempt to rollback
	rollback := func() {
		if m.dnsServer != nil {
			m.dnsServer.SetStore(m.store)
		}
		m.handler.Proxy.Store(false)
	}

	// If we are the leader remove ourselves directly,
	// otherwise request the leader remove us from the peer set.
	if leaderAddr == m.advertiseAddr {
		if err := m.store.RemovePeer(m.advertiseAddr); err != nil {
			rollback()
			return err
		}
	} else {
		if err := discoverd.DefaultClient.RaftRemovePeer(m.advertiseAddr); err != nil {
			rollback()
			return err
		}
	}

	// Close the raft store.
	if m.store != nil {
		m.store.Close()
		m.store = nil
	}
	m.handler.Store = nil

	m.logger.Println("demoted successfully")
	return nil
}

func (m *Main) Deregister() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.hb != nil {
		m.logger.Println("deregistering service")
		return m.hb.Close()
	}
	return nil
}

// Close shuts down all open servers.
func (m *Main) Close() (info dt.TargetLogIndex, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.logger.Println("discoverd shutting down")
	if m.httpServer != nil {
		// Disable keep alives so that persistent connections will close
		m.httpServer.SetKeepAlivesEnabled(false)
	}
	if m.dnsServer != nil {
		m.dnsServer.Close()
		m.dnsServer = nil
	}
	if m.ln != nil {
		m.ln.Close()
		m.ln = nil
	}
	if m.store != nil {
		info.LastIndex, err = m.store.Close()
		m.store = nil
	}
	return info, err
}

// openStore initializes and opens the store.
func (m *Main) openStore() error {
	// If the advertised address is not in the peer list then we should proxy.

	// Resolve advertised address.
	addr, err := net.ResolveTCPAddr("tcp", m.advertiseAddr)
	if err != nil {
		return err
	}

	// Listen via mux
	storeLn := m.mux.Listen([]byte{server.StoreHdr})

	// Initialize store.
	s := server.NewStore(m.dataDir)
	s.Listener = storeLn
	s.Advertise = addr

	// Allow single node if there's no peers set.
	s.EnableSingleNode = len(m.peers) <= 1

	// Open store.
	if err := s.Open(); err != nil {
		return err
	}
	m.store = s

	// If peers then set peer set.
	if len(m.peers) > 0 {
		if err := s.SetPeers(m.peers); err != nil {
			return fmt.Errorf("set peers: %s", err)
		}
	}

	return nil
}

// openDNSServer initializes and opens the DNS server.
// The store must already be open.
func (m *Main) openDNSServer(addr string, recursors []string) error {
	s := &server.DNSServer{
		UDPAddr:   addr,
		TCPAddr:   addr,
		Recursors: recursors,
	}

	// If store is available then attach it. Otherwise use a proxy.
	if m.store != nil {
		s.SetStore(m.store)
	} else {
		s.SetStore(&server.ProxyStore{Peers: m.peers})
	}

	if err := s.ListenAndServe(); err != nil {
		return err
	}
	m.dnsServer = s
	return nil
}

// openHTTPServer initializes and opens the HTTP server.
// The store must already be open.
func (m *Main) openHTTPServer() error {
	h := server.NewHandler(false, m.peers)
	h.Main = m
	h.Peers = m.peers
	// If we have no store then start the handler in proxy mode
	if m.store == nil {
		h.Proxy.Store(true)
	} else {
		h.Store = m.store
	}
	m.handler = h
	m.httpServer = &http.Server{Handler: h}

	// Create listener via mux
	// HTTP listens to all methods: CONNECT, DELETE, GET, HEAD, OPTIONS, POST, PUT, TRACE.
	httpLn := m.mux.Listen([]byte{'C', 'D', 'G', 'H', 'O', 'P', 'T'})
	go m.httpServer.Serve(httpLn)
	return nil
}

// Notify sends a POST to notifyURL to let it know that addr is accessible.
func (m *Main) Notify(notifyURL, dnsAddr string) {
	m.mu.Lock()
	m.status.URL = strings.Join(m.peers, ",")
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
// If timeout is -1 then wait indefinitely
func (m *Main) waitForLeader(timeout time.Duration) error {
	// Ignore leadership if we are a proxy.
	if m.store == nil {
		return nil
	}
	var timeoutCh <-chan time.Time
	if timeout != IndefiniteTimeout {
		timeoutCh = time.After(timeout)
	}
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
	fs.StringVar(&opt.Addr, "addr", ":1111", "address to serve http and raft from")
	fs.StringVar(&opt.DNSAddr, "dns-addr", "", "address to service DNS from")
	fs.StringVar(&recursors, "recursors", "", "upstream recursive DNS servers")
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
	Addr       string   // bind address (raft & http)
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
