package main

import (
	"bufio"
	"bytes"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"strconv"
	"sync"

	"github.com/flynn/flynn/logaggregator/rfc5424"
	"github.com/flynn/flynn/pkg/shutdown"

	"github.com/flynn/flynn/Godeps/_workspace/src/gopkg.in/inconshreveable/log15.v2"
)

func main() {
	defer shutdown.Exit()

	listenPort := os.Getenv("PORT")
	if listenPort == "" {
		listenPort = "5000"
	}

	listenAddr := flag.String("listenaddr", ":"+listenPort, "syslog input listen address")

	a := NewAggregator(*listenAddr)
	if err := a.Start(); err != nil {
		shutdown.Fatal(err)
	}
	shutdown.BeforeExit(a.Shutdown)
	defer shutdown.Exit()
}

// Aggregator is a log aggregation server that collects syslog messages.
type Aggregator struct {
	// Addr is the address (host:port) to listen on for incoming syslog messages.
	Addr string

	listener     net.Listener
	logc         chan []byte
	numConsumers int
	consumerwg   sync.WaitGroup
	producerwg   sync.WaitGroup

	once     sync.Once // protects the following:
	shutdown chan struct{}
}

// NewAggregator creates a new unstarted Aggregator that will listen on addr.
func NewAggregator(addr string) *Aggregator {
	return &Aggregator{
		Addr:         "127.0.0.1:0",
		logc:         make(chan []byte),
		numConsumers: 10,
		shutdown:     make(chan struct{}),
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

	for i := 0; i < a.numConsumers; i++ {
		a.consumerwg.Add(1)
		go func() {
			defer a.consumerwg.Done()
			a.consumeLogs()
		}()
	}

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
		close(a.logc)
		a.consumerwg.Wait()
	})
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

func (a *Aggregator) consumeLogs() {
	for line := range a.logc {
		// TODO: forward message to follower aggregator
		// TODO: parse the message, send it to the right bucket
		fmt.Printf("message received: %q\n", string(line))
		msg, err := rfc5424.Parse(line)
		if err != nil {
			log15.Error("rfc5424 parse error", "err", err)
			continue
		}
		fmt.Printf("MSG: %#v\n", msg)

		if afterMessage != nil {
			afterMessage()
		}
	}
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
	s.Split(rfc6587Split)
	for s.Scan() {
		msg := s.Bytes()
		// slice in msg could get modified on next Scan(), need to copy it
		msgCopy := make([]byte, len(msg))
		copy(msgCopy, msg)
		a.logc <- msgCopy
	}
}

func rfc6587Split(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}

	i := bytes.IndexByte(data, ' ')
	switch {
	case i == 0:
		return 0, nil, errors.New("expected MSG-LEN, got space")
	case i > 5:
		return 0, nil, errors.New("MSG-LEN was longer than 5 characters")
	case i > 0:
		msgLen := data[0:i]
		length, err := strconv.Atoi(string(msgLen))
		if err != nil {
			return 0, nil, err
		}
		if length > 10000 {
			return 0, nil, fmt.Errorf("maximum MSG-LEN is 10000, got %d", length)
		}
		end := length + i + 1
		if len(data) >= end {
			// Return frame without msg length
			return end, data[i+1 : end], nil
		}
	}
	// Request more data.
	return 0, nil, nil
}
