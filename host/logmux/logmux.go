package logmux

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/technoweenie/grohl"

	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/syslog/rfc5424"
	"github.com/flynn/flynn/pkg/syslog/rfc6587"
)

// LogMux collects log lines from multiple readers and forwards them to a log
// aggregator service registered in discoverd. Log lines are buffered in memory
// and are dropped in LIFO order.
type LogMux struct {
	logc chan *rfc5424.Message

	sc *serviceConn

	producerwg *sync.WaitGroup

	shutdowno sync.Once
	shutdownc chan struct{}

	doneo sync.Once
	donec chan struct{}

	msgSeq uint32
}

// New returns an instance of LogMux ready to follow io.Reader producers. The
// log messages are buffered internally until Connect is called.
func New(bufferSize int) *LogMux {
	return &LogMux{
		logc:       make(chan *rfc5424.Message, bufferSize),
		producerwg: &sync.WaitGroup{},
		shutdownc:  make(chan struct{}),
		donec:      make(chan struct{}),
	}
}

// Connect opens a connection to the named log aggregation service in discoverd
// and creates a goroutine that drains the log message buffer to the connection.
func (m *LogMux) Connect(discd *discoverd.Client, name string) error {
	conn, err := connect(discd, name, m.donec)
	if err != nil {
		return err
	}
	m.sc = conn

	go m.drainTo(conn)
	return nil
}

func (m *LogMux) drainTo(w io.Writer) {
	defer close(m.donec)

	g := grohl.NewContext(grohl.Data{"at": "logmux_drain"})

	for {
		msg, ok := <-m.logc
		if !ok {
			return // shutdown
		}

		_, err := w.Write(rfc6587.Bytes(msg))
		if err != nil {
			g.Log(grohl.Data{"status": "error", "err": err.Error()})

			// write logs to local logger when the writer fails
			g.Log(grohl.Data{"msg": msg.String()})
			for msg := range m.logc {
				g.Log(grohl.Data{"msg": msg.String()})
			}

			return // shutdown
		}
	}
}

// Close blocks until all producers have finished, then terminates the drainer,
// and blocks until the backlog in logc has been processed.
func (m *LogMux) Close() {
	m.producerwg.Wait()

	m.doneo.Do(func() { close(m.logc) })

	select {
	case <-m.donec:
	case <-time.NewTimer(3 * time.Second).C:
		// logs did not drain to logaggregator in 3 seconds, drain them to the local logger
		if m.sc != nil {
			close(m.sc.closec)
			<-m.donec
		}
	}
}

type Config struct {
	AppID, HostID, JobID, JobType string
}

// Follow forwards log lines from the reader into the syslog client. Follow
// runs until the reader is closed or an error occurs. If an error occurs, the
// reader may still be open.
func (m *LogMux) Follow(r io.Reader, fd int, config Config) {
	m.producerwg.Add(1)

	hdr := &rfc5424.Header{
		Hostname: []byte(config.HostID),
		AppName:  []byte(config.AppID),
		ProcID:   []byte(config.JobType + "." + config.JobID),
		MsgID:    []byte(fmt.Sprintf("ID%d", fd)),
	}

	go m.follow(r, hdr)
}

func (m *LogMux) follow(r io.Reader, hdr *rfc5424.Header) {
	defer m.producerwg.Done()

	g := grohl.NewContext(grohl.Data{"at": "logmux_follow"})
	s := bufio.NewScanner(r)

	seqBuf := make([]byte, 10)
	sd := &rfc5424.StructuredData{
		ID:     []byte("flynn"),
		Params: []rfc5424.StructuredDataParam{{Name: []byte("seq")}},
	}

	for s.Scan() {
		msg := rfc5424.NewMessage(hdr, s.Bytes())
		sd.Params[0].Value = strconv.AppendUint(seqBuf[:0], uint64(atomic.AddUint32(&m.msgSeq, 1)), 10)
		var sdBuf bytes.Buffer
		sd.Encode(&sdBuf)
		msg.StructuredData = sdBuf.Bytes()

		select {
		case m.logc <- msg:
		default:
			// throw away msg if logc buffer is full
		}
	}

	if s.Err() != nil {
		g.Log(grohl.Data{"status": "error", "err": s.Err()})
	}
}
