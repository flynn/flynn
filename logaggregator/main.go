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
	"github.com/flynn/flynn/logaggregator/snapshot"
	"github.com/flynn/flynn/pkg/shutdown"
	"github.com/flynn/flynn/pkg/syslog/rfc5424"
	"github.com/flynn/flynn/pkg/syslog/rfc6587"

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
	snapshotPath := flag.String("snapshot", "", "snapshot path")
	flag.Parse()

	a := NewAggregator(*logAddr)

	if *snapshotPath != "" {
		if err := a.ReplaySnapshot(*snapshotPath); err != nil {
			shutdown.Fatal(err)
		}

		shutdown.BeforeExit(func() {
			if err := a.TakeSnapshot(*snapshotPath); err != nil {
				log15.Error("snapshot error", "err", err)
			}
		})
	}

	if err := a.Start(); err != nil {
		shutdown.Fatal(err)
	}
	shutdown.BeforeExit(a.Shutdown)

	listener, err := net.Listen("tcp4", *apiAddr)
	if err != nil {
		shutdown.Fatal(err)
	}

	hb, err := discoverd.AddServiceAndRegister("flynn-logaggregator", *logAddr)
	if err != nil {
		shutdown.Fatal(err)
	}
	shutdown.BeforeExit(func() { hb.Close() })

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

	msgc chan *rfc5424.Message

	once     sync.Once // protects the following:
	shutdown chan struct{}
}

// NewAggregator creates a new unstarted Aggregator that will listen on addr.
func NewAggregator(addr string) *Aggregator {
	a := &Aggregator{
		Addr:     addr,
		buffers:  make(map[string]*ring.Buffer),
		shutdown: make(chan struct{}),
		msgc:     make(chan *rfc5424.Message),
	}

	go a.run()
	return a
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
		close(a.msgc)
	})
}

// ReadLastN reads up to N logs from the log buffer with id and sends them over
// a channel. If n is less than 0, or if there are fewer than n logs buffered,
// all buffered logs are returned. If a signal is sent on done, the returned
// channel is closed and the goroutine exits.
func (a *Aggregator) ReadLastN(
	id string,
	n int,
	filter Filter,
	done <-chan struct{},
) <-chan *rfc5424.Message {
	msgc := make(chan *rfc5424.Message)
	go func() {
		defer close(msgc)

		messages := filter.Filter(a.readLastN(id, -1))
		if n > 0 && len(messages) > n {
			messages = messages[len(messages)-n:]

		}
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

func (a *Aggregator) TakeSnapshot(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	// TODO(benburkert): restructure Aggregator & ring.Buffer to avoid nested locks
	a.bmu.Lock()
	bufs := make([][]*rfc5424.Message, 0, len(a.buffers))
	for _, buf := range a.buffers {
		bufs = append(bufs, buf.Clone().ReadAll())
	}
	a.bmu.Unlock()

	return snapshot.Take(bufs, f)
}

func (a *Aggregator) ReplaySnapshot(path string) error {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}

	s := snapshot.NewScanner(f)
	for s.Scan() {
		a.msgc <- s.Message
	}
	return s.Err()
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
func (a *Aggregator) ReadLastNAndSubscribe(
	id string,
	n int,
	filter Filter,
	done <-chan struct{},
) <-chan *rfc5424.Message {
	msgc := make(chan *rfc5424.Message)
	go func() {
		buf := a.getOrInitializeBuffer(id)

		var messages []*rfc5424.Message
		var subc <-chan *rfc5424.Message
		var cancel func()

		if (filter != nil && n != 0) || n < 0 {
			messages, subc, cancel = buf.ReadAllAndSubscribe()
		} else {
			messages, subc, cancel = buf.ReadLastNAndSubscribe(n)
		}
		if filter != nil {
			messages = filter.Filter(messages)
			if n > 0 && len(messages) > n {
				messages = messages[len(messages)-n:]
			}
		}
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
				if !filter.Match(msg) {
					continue // skip this message if it doesn't match filters
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

// testing hook:
var afterMessage func()

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
		} else {
			a.msgc <- msg
		}
	}
}

func (a *Aggregator) run() {
	for msg := range a.msgc {
		a.getOrInitializeBuffer(string(msg.AppName)).Add(msg)
		if afterMessage != nil {
			afterMessage()
		}
	}
}
