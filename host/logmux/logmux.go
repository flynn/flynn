package logmux

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/flynn/flynn/Godeps/_workspace/src/gopkg.in/inconshreveable/log15.v2"
	"github.com/flynn/flynn/Godeps/_workspace/src/gopkg.in/natefinch/lumberjack.v2"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/logaggregator/client"
	"github.com/flynn/flynn/logaggregator/utils"
	"github.com/flynn/flynn/pkg/stream"
	"github.com/flynn/flynn/pkg/syslog/rfc5424"
	"github.com/flynn/flynn/pkg/syslog/rfc6587"
)

type message struct {
	*utils.HostCursor
	*rfc5424.Message
}

// LogMux collects log lines from multiple leaders and forwards them to
// logaggregator instances and local files.
type Mux struct {
	hostID string
	logDir string
	logger log15.Logger

	msgSeq uint32

	jobsMtx sync.Mutex
	// jobWaits stores WaitGroups for jobs that we're currently following
	jobWaits map[string]*sync.WaitGroup
	// jobStarts stores a channel to signal followers waiting for us to start
	// following a job, in order to wait on the appropriate group from jobWaits,
	// as a WaitGroup can't be waited until the counter is >0
	jobStarts map[string]chan struct{}

	subscribersMtx sync.RWMutex
	subscribers    map[string]map[chan message]struct{}

	appLogsMtx sync.Mutex
	appLogs    map[string]*appLog
}

const firehoseApp = "_all"

func New(hostID, logDir string, logger log15.Logger) *Mux {
	return &Mux{
		hostID:      hostID,
		logDir:      logDir,
		logger:      logger,
		jobWaits:    make(map[string]*sync.WaitGroup),
		jobStarts:   make(map[string]chan struct{}),
		subscribers: make(map[string]map[chan message]struct{}),
		appLogs:     make(map[string]*appLog),
	}
}

type Config struct {
	AppID, HostID, JobID, JobType string
}

func (m *Mux) StreamToAggregators(s discoverd.Service) error {
	l := m.logger.New("fn", "StreamToAggregators")
	ch := make(chan *discoverd.Event)
	_, err := s.Watch(ch)
	if err != nil {
		l.Error("failed to connect to discoverd watch", "error", err)
		return err
	}
	m.logger.Info("connected to discoverd watch")
	go func() {
		for e := range ch {
			if e.Kind != discoverd.EventKindUp {
				continue
			}
			l.Info("connecting to new aggregator", "addr", e.Instance.Addr)
			go m.addAggregator(e.Instance.Addr)
		}
	}()
	return nil
}

func (m *Mux) subscribe(app string, ch chan message) func() {
	m.subscribersMtx.Lock()
	defer m.subscribersMtx.Unlock()
	subs, ok := m.subscribers[app]
	if !ok {
		subs = make(map[chan message]struct{})
		m.subscribers[app] = subs
	}
	subs[ch] = struct{}{}
	return func() {
		go func() {
			// drain channel to prevent deadlock
			for range ch {
			}
		}()
		m.subscribersMtx.Lock()
		defer m.subscribersMtx.Unlock()
		delete(m.subscribers[app], ch)
	}
}

func (m *Mux) broadcast(app string, msg message) {
	m.subscribersMtx.RLock()
	defer m.subscribersMtx.RUnlock()

	for ch := range m.subscribers[firehoseApp] {
		ch <- msg
		// TODO: if blocking write, drop+notify, eventually close
	}
	for ch := range m.subscribers[app] {
		ch <- msg
	}
}

func (m *Mux) addAggregator(addr string) {
	l := m.logger.New("fn", "addAggregator", "addr", addr)
	// TODO(titanous): add dial timeout
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		l.Error("failed to connect to aggregator", "error", err)
		return
	}
	l.Info("connected to aggregator")

	host, _, _ := net.SplitHostPort(addr)
	c, _ := client.New("http://" + host)
	cursors, err := c.GetCursors()
	if err != nil {
		// TODO(titanous): retry
		l.Error("failed to get cursors from aggregator", "error", err)
		conn.Close()
		return
	}

	var aggCursor *utils.HostCursor
	if c, ok := cursors[m.hostID]; ok {
		aggCursor = &c
	}
	if aggCursor != nil {
		l.Info("got cursor", "cursor.timestamp", aggCursor.Time, "cursor.seq", aggCursor.Seq)
	} else {
		l.Info("no cursor for host")
	}

	appLogs, err := m.logFiles("")
	if err != nil {
		l.Error("failed to get local log files", "error", err)
		conn.Close()
		return
	}

	bufferedMessages := make(chan message)
	firehose := make(chan message)
	done := make(chan struct{})

	// subscribe to all messages
	unsubscribe := m.subscribe(firehoseApp, firehose)

	bufferCursors := make(map[string]utils.HostCursor)
	var bufferCursorsMtx sync.Mutex
	go func() {
		l := m.logger.New("fn", "sendToAggregator", "addr", addr)
		defer unsubscribe()
		defer conn.Close()
		defer close(done)
		bm := bufferedMessages // make a copy so we can nil it later
		for {
			var m message
			var ok bool
			select {
			case m, ok = <-bm:
				if !ok {
					bm = nil
					continue
				}
			case m, ok = <-firehose:
				if !ok {
					return
				}

				// if app in list of app logs and cursor from reading files, skip
				appID := string(m.Message.AppName)
				if _, ok := appLogs[appID]; ok {
					bufferCursorsMtx.Lock()
					c, ok := bufferCursors[appID]
					bufferCursorsMtx.Unlock()
					if !ok || c.After(*m.HostCursor) {
						continue
					}
				}
			}
			if string(m.MsgID) == "ID3" {
				continue
			}
			if _, err := conn.Write(rfc6587.Bytes(m.Message)); err != nil {
				l.Error("failed to write message", "error", err)
				return
			}
		}
	}()

	for appID, logs := range appLogs {
		for i, name := range logs {
			func() {
				l := l.New("log", name)
				f, err := os.Open(name)
				if err != nil {
					l.Error("failed to open log file", "error", err)
					return
				}
				defer f.Close()
				sc := bufio.NewScanner(f)
				sc.Split(rfc6587.SplitWithNewlines)
				var cursor *utils.HostCursor
				cursorSaved := false
			scan:
				for sc.Scan() {
					msgBytes := sc.Bytes()
					// slice in msgBytes could get modified on next Scan(), need to copy it
					msgCopy := make([]byte, len(msgBytes)-1)
					copy(msgCopy, msgBytes)
					var msg *rfc5424.Message
					msg, cursor, err = utils.ParseMessage(msgCopy)
					if err != nil {
						l.Error("failed to parse message", "msg", string(msgCopy), "error", err)
						continue
					}
					if aggCursor != nil && !cursor.After(*aggCursor) {
						continue
					}
					select {
					case bufferedMessages <- message{cursor, msg}:
					case <-done:
						return
					}
				}
				if err := sc.Err(); err != nil {
					l.Error("failed to scan message", "error", err)
					return
				}
				if !cursorSaved && i == len(appLogs[appID])-1 {
					// last file, send cursor to processing goroutine
					bufferCursorsMtx.Lock()
					bufferCursors[appID] = *cursor
					bufferCursorsMtx.Unlock()
					cursorSaved = true
					// read to end of file again
					goto scan
				}
			}()
		}
	}
	close(bufferedMessages)
}

// Follow starts a goroutine that reads log lines from the reader into the mux.
// It runs until the reader is closed or an error occurs. If an error occurs,
// the reader may still be open.
func (m *Mux) Follow(r io.ReadCloser, buffer string, fd int, config Config) *LogStream {
	hdr := &rfc5424.Header{
		Hostname: []byte(config.HostID),
		AppName:  []byte(config.AppID),
		MsgID:    []byte(fmt.Sprintf("ID%d", fd)),
	}
	if config.JobType != "" {
		hdr.ProcID = []byte(config.JobType + "." + config.JobID)
	} else {
		hdr.ProcID = []byte(config.JobID)
	}

	s := &LogStream{
		m:    m,
		log:  r,
		done: make(chan struct{}),
	}
	s.closed.Store(true)

	m.jobsMtx.Lock()
	defer m.jobsMtx.Unlock()
	// set up the WaitGroup so that subscribers can track when the job fds have closed
	wg, ok := m.jobWaits[config.JobID]
	if !ok {
		wg = &sync.WaitGroup{}
		m.jobWaits[config.JobID] = wg
	}
	wg.Add(1)
	if !ok {
		// we created the wg, so create a goroutine to clean up
		go func() {
			wg.Wait()
			m.jobsMtx.Lock()
			defer m.jobsMtx.Unlock()
			delete(m.jobWaits, config.JobID)
		}()
	}

	// if there is a jobStart channel, a subscriber is waiting for the WaitGroup
	// to be created, signal it.
	if ch, ok := m.jobStarts[config.JobID]; ok {
		close(ch)
		delete(m.jobStarts, config.JobID)
	}

	go s.follow(r, buffer, config.AppID, hdr, wg)
	return s
}

type LogStream struct {
	m      *Mux
	log    io.Closer
	buf    string
	closed atomic.Value // bool
	done   chan struct{}
}

func (s *LogStream) Close() string {
	s.closed.Store(true)
	s.log.Close()
	<-s.done
	return s.buf
}

func (s *LogStream) follow(r io.Reader, buffer, appID string, h *rfc5424.Header, wg *sync.WaitGroup) {
	defer wg.Done()
	defer close(s.done)
	l := s.m.appLog(appID)
	seqBuf := make([]byte, 10)
	sd := &rfc5424.StructuredData{
		ID:     []byte("flynn"),
		Params: []rfc5424.StructuredDataParam{{Name: []byte("seq")}},
	}

	br := bufio.NewReaderSize(io.MultiReader(strings.NewReader(buffer), r), 10000)
	for {
		line, _, err := br.ReadLine()
		if err != nil {
			// if the log was explicitly closed (because an update
			// is in progress), store the buffer and return so it
			// can be passed to the new flynn-host daemon.
			if s.closed.Load().(bool) {
				remaining, _ := br.Peek(br.Buffered())
				s.buf = string(remaining)
			}
			return
		}
		msg := rfc5424.NewMessage(h, line)
		cursor := &utils.HostCursor{
			Time: msg.Timestamp,
			Seq:  uint64(atomic.AddUint32(&s.m.msgSeq, 1)),
		}
		sd.Params[0].Value = strconv.AppendUint(seqBuf[:0], cursor.Seq, 10)
		var sdBuf bytes.Buffer
		sd.Encode(&sdBuf)
		msg.StructuredData = sdBuf.Bytes()
		l.Write(message{cursor, msg})
	}
}

func (m *Mux) StreamLog(appID, jobID string, history, follow bool, ch chan<- *rfc5424.Message) (stream.Stream, error) {
	if history {
		return m.streamWithHistory(appID, jobID, follow, ch)
	}
	return m.followLog(appID, jobID, ch)
}

// jobDoneCh returns a channel that is closed when all of the streams we are
// following from the job have been closed. It will never unblock if the job has
// already finished.
func (m *Mux) jobDoneCh(jobID string, stop <-chan struct{}) <-chan struct{} {
	ch := make(chan struct{})
	go func() {
		defer close(ch)
		var started chan struct{}
		m.jobsMtx.Lock()
		// check if there is already a WaitGroup
		wg, ok := m.jobWaits[jobID]
		if !ok {
			// if not, check if there is a channel to be notified when one is
			// created
			started, ok = m.jobStarts[jobID]
			if !ok {
				// if not, make and save the channel
				started = make(chan struct{})
				m.jobStarts[jobID] = started
			}
		}
		m.jobsMtx.Unlock()

		if started != nil {
			// if the wg doesn't exist, wait for it
			select {
			case <-started:
			case <-stop:
				return
			}
			m.jobsMtx.Lock()
			wg, ok = m.jobWaits[jobID]
			m.jobsMtx.Unlock()
			if !ok {
				// if there is no wg, it was created and deleted before we could
				// read it, we're done
				return
			}
		}

		// wait for the job to finish
		wg.Wait()
	}()
	return ch
}

func (m *Mux) followLog(appID, jobID string, ch chan<- *rfc5424.Message) (stream.Stream, error) {
	s := stream.New()
	var jobDone <-chan struct{}
	if jobID != "" {
		jobDone = m.jobDoneCh(jobID, s.StopCh)
	}
	go func() {
		msgs := make(chan message)
		unsubscribe := m.subscribe(appID, msgs)
		defer unsubscribe()
		defer close(ch)
		for {
			select {
			case msg, ok := <-msgs:
				if !ok {
					return
				}
				if jobID != "" && !strings.HasSuffix(string(msg.Message.Header.ProcID), jobID) {
					// skip messages that aren't from the job we care about
					continue
				}
				select {
				case ch <- msg.Message:
				case <-s.StopCh:
					return
				}
			case <-s.StopCh:
				return
			case <-jobDone:
				return
			}
		}
	}()
	return s, nil
}

func (m *Mux) streamWithHistory(appID, jobID string, follow bool, ch chan<- *rfc5424.Message) (stream.Stream, error) {
	l := m.logger.New("fn", "streamWithHistory", "app.id", appID, "job.id", jobID)
	logs, err := m.logFiles(appID)
	if err != nil {
		return nil, err
	}
	if len(logs) == 0 {
		return m.followLog(appID, jobID, ch)
	}

	msgs := make(chan message)
	unsubscribeFn := make(chan func(), 1)

	s := stream.New()
	var jobDone <-chan struct{}
	if jobID != "" {
		jobDone = m.jobDoneCh(jobID, s.StopCh)
	}

	go func() {
		var cursor *utils.HostCursor
		var unsubscribe func()
		var done bool
		defer func() {
			close(ch)
			if unsubscribe != nil {
				unsubscribe()
			}
		}()
		for {
			select {
			case msg, ok := <-msgs:
				if !ok {
					return
				}
				if jobID != "" && !strings.HasSuffix(string(msg.Message.Header.ProcID), jobID) {
					// skip messages that aren't from the job we care about
					continue
				}
				if cursor != nil && !msg.HostCursor.After(*cursor) {
					// skip messages with old cursors
					continue
				}
				cursor = msg.HostCursor
				select {
				case ch <- msg.Message:
				case <-s.StopCh:
					return
				}
			case <-jobDone:
				if unsubscribe != nil {
					return
				}
				// we haven't finished reading the historical logs, exit when finished
				done = true
				jobDone = nil
			case fn, ok := <-unsubscribeFn:
				if !ok {
					if done {
						// historical logs done, and job already exited
						return
					}
					unsubscribeFn = nil
					continue
				}
				unsubscribe = fn
			case <-s.StopCh:
				return
			}
		}
	}()

	go func() {
		defer close(unsubscribeFn)
		for i, name := range logs[appID] {
			func() {
				l := l.New("log", name)
				f, err := os.Open(name)
				if err != nil {
					l.Error("error reading log", "error", err)
				}
				defer f.Close()
				sc := bufio.NewScanner(f)
				sc.Split(rfc6587.SplitWithNewlines)
				var eof bool
			scan:
				for sc.Scan() {
					msgBytes := sc.Bytes()
					// slice in msgBytes could get modified on next Scan(), need to copy it
					msgCopy := make([]byte, len(msgBytes)-1)
					copy(msgCopy, msgBytes)
					msg, cursor, err := utils.ParseMessage(msgCopy)
					if err != nil {
						l.Error("error parsing log message", "error", err)
						continue
					}
					select {
					case msgs <- message{cursor, msg}:
					case <-s.StopCh:
						return
					}
				}
				if err := sc.Err(); err != nil {
					l.Error("error scanning log message", "error", err)
					s.Error = err
					close(ch)
				}
				if follow && !eof && i == len(logs[appID])-1 {
					// got EOF on last file, subscribe to stream
					eof = true
					unsubscribeFn <- m.subscribe(appID, msgs)
					// read to end of file again
					goto scan
				}
			}()
			if !follow {
				close(msgs)
			}
		}
	}()

	return s, nil
}

var appIDPrefixPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)

// logFiles returns a list of app IDs and the list of log file names associated
// with them, from oldest to newest. There is always at least one file, the
// current log for the app.
func (m *Mux) logFiles(app string) (map[string][]string, error) {
	files, err := ioutil.ReadDir(m.logDir)
	if err != nil {
		return nil, err
	}

	res := make(map[string][]string)
	for _, f := range files {
		n := f.Name()
		if f.IsDir() || !strings.HasSuffix(n, ".log") || !appIDPrefixPattern.MatchString(n) || !strings.HasPrefix(n, app) {
			continue
		}
		appID := n[:36]
		res[appID] = append(res[appID], filepath.Join(m.logDir, n))
	}

	return res, nil
}

func (m *Mux) appLog(id string) *appLog {
	m.appLogsMtx.Lock()
	defer m.appLogsMtx.Unlock()

	// if existing, return it and increment reference count
	if l, ok := m.appLogs[id]; ok {
		l.mtx.Lock()
		defer l.mtx.Unlock()
		if l.refs > 0 {
			l.refs++
			return l
		}
		// if refs == 0, it was closed before we got it, create a new one
	}

	// if not, create log
	l := &appLog{
		appID: id,
		m:     m,
		refs:  1,
		l: &lumberjack.Logger{
			Filename:   filepath.Join(m.logDir, id+".log"),
			MaxBackups: 1,
		},
	}
	m.appLogs[id] = l
	return l
}

type appLog struct {
	appID string
	m     *Mux
	l     *lumberjack.Logger

	mtx  sync.Mutex
	refs int
}

func (l *appLog) Write(msg message) {
	l.l.Write(append(rfc6587.Bytes(msg.Message), '\n'))
	l.m.broadcast(l.appID, msg)
}

// Release releases the app log, when the last job releases an app log, it is
// closed.
func (l *appLog) Release() {
	l.mtx.Lock()
	defer l.mtx.Unlock()
	l.refs--
	if l.refs == 0 {
		// we're the last user, clean it up
		l.l.Close()
		l.m.appLogsMtx.Lock()
		delete(l.m.appLogs, l.appID)
		l.m.appLogsMtx.Unlock()
	}
}
