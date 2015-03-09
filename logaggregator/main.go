package main

import (
	"bufio"
	"flag"
	"net"
	"net/http"
	"os"
	"sync"

	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/logaggregator/ring"
	"github.com/flynn/flynn/pkg/shutdown"
	"github.com/flynn/flynn/pkg/syslog/rfc5424"
	"github.com/flynn/flynn/pkg/syslog/rfc6587"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/kavu/go_reuseport"
	"github.com/flynn/flynn/Godeps/_workspace/src/gopkg.in/inconshreveable/log15.v2"
)

func main() {
	defer shutdown.Exit()

	apiPort := os.Getenv("PORT")
	if apiPort == "" {
		apiPort = "5000"
	}

	logAddr := flag.String("logaddr", ":3000", "syslog input listen address")
	apiAddr := flag.String("apiaddr", ":"+apiPort, "api listen address")
	flag.Parse()

	a := NewAggregator(*logAddr)
	if err := a.Start(); err != nil {
		shutdown.Fatal(err)
	}
	shutdown.BeforeExit(a.Shutdown)

	listener, err := reuseport.NewReusablePortListener("tcp4", *apiAddr)
	if err != nil {
		shutdown.Fatal(err)
	}

	services := map[string]string{
		"flynn-logaggregator-api":    *apiAddr,
		"flynn-logaggregator-syslog": *logAddr,
	}
	for service, addr := range services {
		hb, err := discoverd.AddServiceAndRegister(service, addr)
		if err != nil {
			shutdown.Fatal(err)
		}
		shutdown.BeforeExit(func() { hb.Close() })
	}

	shutdown.Fatal(http.Serve(listener, apiHandler(a)))
}

// Aggregator is a log aggregation server that collects syslog messages.
type Aggregator struct {
	// Addr is the address (host:port) to listen on for incoming syslog messages.
	Addr string

	bmu        sync.Mutex // protects buffers
	buffers    map[string]*ring.Buffer
	listener   net.Listener
	producerwg sync.WaitGroup

	once     sync.Once // protects the following:
	shutdown chan struct{}
}

// NewAggregator creates a new unstarted Aggregator that will listen on addr.
func NewAggregator(addr string) *Aggregator {
	return &Aggregator{
		Addr:     addr,
		buffers:  make(map[string]*ring.Buffer),
		shutdown: make(chan struct{}),
	}
}

// Start starts the Aggregator on Addr.
func (a *Aggregator) Start() error {
	var err error
	a.listener, err = net.Listen("tcp", a.Addr)
	if err != nil {
		return err
	}
	a.Addr = a.listener.Addr().String()

	a.producerwg.Add(1)
	go func() {
		defer a.producerwg.Done()
		a.accept()
	}()
	return nil
}

// Shutdown shuts down the Aggregator gracefully by closing its listener,
// and waiting for already-received logs to be processed.
func (a *Aggregator) Shutdown() {
	a.once.Do(func() {
		close(a.shutdown)
		a.listener.Close()
		a.producerwg.Wait()
	})
}

// ReadLastN reads up to N logs from the log buffer with id and sends them over
// a channel. If n is less than 0, or if there are fewer than n logs buffered,
// all buffered logs are returned. If a signal is sent on done, the returned
// channel is closed and the goroutine exits.
func (a *Aggregator) ReadLastN(id string, n int, done <-chan struct{}) <-chan *rfc5424.Message {
	msgc := make(chan *rfc5424.Message)
	go func() {
		defer close(msgc)

		messages := a.readLastN(id, n)
		for _, syslogMsg := range messages {
			select {
			case msgc <- syslogMsg:
			case <-done:
				return
			}
		}
	}()
	return msgc
}

// readLastN reads up to N logs from the log buffer with id. If n is less than
// 0, or if there are fewer than n logs buffered, all buffered logs are
// returned.
func (a *Aggregator) readLastN(id string, n int) []*rfc5424.Message {
	buf := a.getBuffer(id)
	if buf == nil {
		return nil
	}
	if n >= 0 {
		return buf.ReadLastN(n)
	}
	return buf.ReadAll()
}

// ReadLastNAndSubscribe is like ReadLastN, except that after sending buffered
// log lines, it also streams new lines as they arrive.
func (a *Aggregator) ReadLastNAndSubscribe(id string, n int, done <-chan struct{}) <-chan *rfc5424.Message {
	msgc := make(chan *rfc5424.Message)
	go func() {
		buf := a.getOrInitializeBuffer(id)
		messages, subc, cancel := buf.ReadLastNAndSubscribe(n)
		defer cancel()
		defer close(msgc)

		// range over messages, watch done
		for _, msg := range messages {
			select {
			case <-done:
				return
			case msgc <- msg:
			}
		}

		// select on subc, done, and cancel if done
		for {
			select {
			case msg := <-subc:
				if msgc == nil { // subc was closed
					return
				}
				select {
				case msgc <- msg:
				case <-done:
					return
				}
			case <-done:
				return
			}
		}
	}()
	return msgc
}

func (a *Aggregator) accept() {
	defer a.listener.Close()

	for {
		select {
		case <-a.shutdown:
			return
		default:
		}
		conn, err := a.listener.Accept()
		if err != nil {
			continue
		}

		a.producerwg.Add(1)
		go func() {
			defer a.producerwg.Done()
			a.readLogsFromConn(conn)
		}()
	}
}

func (a *Aggregator) getBuffer(id string) *ring.Buffer {
	a.bmu.Lock()
	defer a.bmu.Unlock()

	buf, _ := a.buffers[id]
	return buf
}

func (a *Aggregator) getOrInitializeBuffer(id string) *ring.Buffer {
	a.bmu.Lock()
	defer a.bmu.Unlock()

	if buf, ok := a.buffers[id]; ok {
		return buf
	}
	buf := ring.NewBuffer()
	a.buffers[id] = buf
	return buf
}

// testing hook:
var afterMessage func()

func (a *Aggregator) readLogsFromConn(conn net.Conn) {
	defer conn.Close()

	connDone := make(chan struct{})
	defer close(connDone)

	go func() {
		select {
		case <-connDone:
		case <-a.shutdown:
			conn.Close()
		}
	}()

	s := bufio.NewScanner(conn)
	s.Split(rfc6587.Split)
	for s.Scan() {
		msgBytes := s.Bytes()
		// slice in msgBytes could get modified on next Scan(), need to copy it
		msgCopy := make([]byte, len(msgBytes))
		copy(msgCopy, msgBytes)

		msg, err := rfc5424.Parse(msgCopy)
		if err != nil {
			log15.Error("rfc5424 parse error", "err", err)
			continue
		}

		a.getOrInitializeBuffer(string(msg.AppName)).Add(msg)
		if afterMessage != nil {
			afterMessage()
		}
	}
}
