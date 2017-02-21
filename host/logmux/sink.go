package logmux

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/boltdb/bolt"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/logaggregator/client"
	"github.com/flynn/flynn/logaggregator/utils"
	"github.com/flynn/flynn/pkg/dialer"
	hh "github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/lru"
	"github.com/flynn/flynn/pkg/syslog/rfc5424"
	"github.com/flynn/flynn/pkg/syslog/rfc6587"
	"github.com/flynn/flynn/pkg/tlsconfig"
	"github.com/julienschmidt/httprouter"
	"gopkg.in/inconshreveable/log15.v2"
)

var SinkExistsError = errors.New("sink with that id already exists")
var SinkNotFoundError = errors.New("sink with that id couldn't be found")

type SinkManager struct {
	mtx    sync.RWMutex
	mux    *Mux
	logger log15.Logger
	sinks  map[string]Sink
	state  JobStateGetter

	dbPath string
	db     *bolt.DB

	shutdownOnce sync.Once
	shutdownCh   chan struct{}
}

type JobStateGetter interface {
	GetJob(id string) *host.ActiveJob
}

func NewSinkManager(dbPath string, mux *Mux, state JobStateGetter, logger log15.Logger) *SinkManager {
	return &SinkManager{
		dbPath:     dbPath,
		mux:        mux,
		logger:     logger,
		sinks:      make(map[string]Sink),
		state:      state,
		shutdownCh: make(chan struct{}),
	}
}

type SinkHTTPAPI struct {
	sm *SinkManager
}

func (s *SinkHTTPAPI) GetSinks(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
	s.sm.mtx.RLock()
	sinks := make([]*ct.Sink, 0, len(s.sm.sinks))
	for _, s := range s.sm.sinks {
		info := s.Info()
		sinks = append(sinks, &ct.Sink{
			ID:          info.ID,
			Kind:        info.Kind,
			Config:      info.Config,
			HostManaged: info.HostManaged,
		})
	}
	s.sm.mtx.RUnlock()
	hh.JSON(w, 200, sinks)
}

func (s *SinkHTTPAPI) AddSink(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
	var info SinkInfo
	if err := hh.DecodeJSON(req, &info); err != nil {
		hh.Error(w, err)
		return
	}
	err := s.sm.AddSink(ps.ByName("id"), &info)
	if err != nil && err != SinkExistsError {
		hh.Error(w, err)
		return
	}
	hh.JSON(w, 200, info)
}

func (s *SinkHTTPAPI) RemoveSink(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
	err := s.sm.RemoveSink(ps.ByName("id"))
	if err == SinkNotFoundError {
		hh.ObjectNotFoundError(w, err.Error())
		return
	} else if err != nil {
		hh.Error(w, err)
		return
	}
	hh.JSON(w, 200, struct{}{})
}

func (sm *SinkManager) RegisterRoutes(r *httprouter.Router) {
	api := &SinkHTTPAPI{sm}
	r.GET("/sinks", api.GetSinks)
	r.PUT("/sinks/:id", api.AddSink)
	r.DELETE("/sinks/:id", api.RemoveSink)
}

func (sm *SinkManager) OpenDB() error {
	sm.mtx.Lock()
	defer sm.mtx.Unlock()
	if sm.dbPath == "" {
		return nil
	}

	// open database file
	if err := os.MkdirAll(filepath.Dir(sm.dbPath), 0755); err != nil {
		return fmt.Errorf("could not mkdir for sink persistence db: %s", err)
	}
	db, err := bolt.Open(sm.dbPath, 0600, &bolt.Options{Timeout: 5 * time.Second})
	if err != nil {
		return fmt.Errorf("could not open sink persistence db: %s", err)
	}

	// create buckets if they don't already exist
	if err := db.Update(func(tx *bolt.Tx) error {
		tx.CreateBucketIfNotExists([]byte("sinks"))
		return nil
	}); err != nil {
		return fmt.Errorf("could not initialise sink persistence db: %s", err)
	}
	sm.db = db

	// restore previous state if any
	if err := sm.restore(); err != nil {
		return err
	}

	// start persistence routine
	go sm.persistSinks()
	return nil
}

func (sm *SinkManager) CloseDB() error {
	// Shutdown persistence routine
	sm.shutdownOnce.Do(func() {
		close(sm.shutdownCh)
	})

	sm.mtx.Lock()
	defer sm.mtx.Unlock()

	for id, s := range sm.sinks {
		s.Shutdown()
		sm.persistSink(id)
	}

	if sm.db == nil {
		return nil
	}
	if err := sm.db.Close(); err != nil {
		return err
	}
	sm.db = nil
	return nil
}

type SinkInfo struct {
	ID          string            `json:"id"`
	Kind        ct.SinkKind       `json:"kind"`
	Cursor      *utils.HostCursor `json:"cursor,omitempty"`
	Config      json.RawMessage   `json:"config"`
	HostManaged bool              `json:"host_managed"`
}

func (sm *SinkManager) restore() error {
	// read back from buckets into in-memory structure
	return sm.db.View(func(tx *bolt.Tx) error {
		sinkBucket := tx.Bucket([]byte("sinks"))
		return sinkBucket.ForEach(func(k, v []byte) error {
			sinkInfo := &SinkInfo{}
			if err := json.Unmarshal(v, sinkInfo); err != nil {
				return fmt.Errorf("failed to deserialize sink info: %s", err)
			}
			return sm.addSink(string(k), sinkInfo, false)
		})
	})
}

func (sm *SinkManager) newSink(s *SinkInfo) (Sink, error) {
	switch s.Kind {
	case ct.SinkKindLogaggregator:
		return NewLogAggregatorSink(sm, s)
	case ct.SinkKindSyslog:
		return NewSyslogSink(sm, s)
	default:
		return nil, fmt.Errorf("unknown sink kind: %q", s.Kind)
	}
}

var ErrDBClosed = errors.New("sink DB closed")

func (sm *SinkManager) persistSink(id string) error {
	if sm.db == nil {
		return ErrDBClosed
	}
	if err := sm.db.Update(func(tx *bolt.Tx) error {
		sinkBucket := tx.Bucket([]byte("sinks"))
		k := []byte(id)

		// remove sink from database if not found in current sinks
		sink, sinkExists := sm.sinks[id]
		if !sinkExists {
			sinkBucket.Delete(k)
			return nil
		}

		// serialize sink info and persist to disk
		b, err := json.Marshal(sink.Info())
		if err != nil {
			return fmt.Errorf("failed to serialize sink info: %s", err)
		}
		err = sinkBucket.Put(k, b)
		if err != nil {
			return fmt.Errorf("failed to persist sink info to boltdb: %s", err)
		}
		return nil
	}); err != nil {
		return err
	}
	return nil
}

func (sm *SinkManager) persistSinks() {
	log := sm.logger.New("fn", "persistSinks")
	log.Info("starting sink persistence routine")
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-sm.shutdownCh:
			return
		case <-ticker.C:
			sm.mtx.Lock()
			for id := range sm.sinks {
				if err := sm.persistSink(id); err != nil {
					log.Warn("error persisting sink", "sink.id", id, "err", err)
				}
			}
			sm.mtx.Unlock()
		}
	}
}

func (sm *SinkManager) AddSink(id string, s *SinkInfo) error {
	sm.mtx.Lock()
	defer sm.mtx.Unlock()

	return sm.addSink(id, s, true)
}

func (sm *SinkManager) addSink(id string, s *SinkInfo, persist bool) error {
	if _, ok := sm.sinks[id]; ok {
		// TODO: handle sink config change
		return SinkExistsError
	}
	sink, err := sm.newSink(s)
	if err != nil {
		return err
	}
	sm.sinks[id] = sink
	if persist {
		if err := sm.persistSink(id); err != nil {
			return err
		}
	}
	go sm.mux.addSink(sink)
	return nil
}

func (sm *SinkManager) RemoveSink(id string) error {
	sm.mtx.Lock()
	defer sm.mtx.Unlock()

	return sm.removeSink(id)
}

func (sm *SinkManager) removeSink(id string) error {
	if s, ok := sm.sinks[id]; !ok {
		return SinkNotFoundError
	} else {
		s.Shutdown()
	}
	delete(sm.sinks, id)
	return sm.persistSink(id)
}

func (sm *SinkManager) StreamToAggregators(s discoverd.Service) error {
	log := sm.logger.New("fn", "StreamToAggregators")
	ch := make(chan *discoverd.Event)
	_, err := s.Watch(ch)
	if err != nil {
		log.Error("failed to connect to discoverd watch", "error", err)
		return err
	}
	log.Info("connected to discoverd watch")
	sm.mtx.RLock()
	initial := make(map[string]struct{})
	for id, sink := range sm.sinks {
		if sink.Info().Kind == ct.SinkKindLogaggregator && sink.Info().HostManaged == true {
			initial[id] = struct{}{}
		}
	}
	sm.mtx.RUnlock()

	go func() {
		for e := range ch {
			switch e.Kind {
			case discoverd.EventKindUp:
				if _, ok := initial[e.Instance.Addr]; ok {
					delete(initial, e.Instance.Addr)
					continue // skip adding as we already have this sink.
				}
				log.Info("connecting to new aggregator", "addr", e.Instance.Addr)
				cfg, _ := json.Marshal(ct.LogAggregatorSinkConfig{Addr: e.Instance.Addr})
				info := &SinkInfo{ID: e.Instance.Addr, Kind: ct.SinkKindLogaggregator, Config: cfg, HostManaged: true}
				sm.AddSink(e.Instance.Addr, info)
			case discoverd.EventKindDown:
				log.Info("disconnecting from aggregator", "addr", e.Instance.Addr)
				sm.RemoveSink(e.Instance.Addr)
			case discoverd.EventKindCurrent:
				for id := range initial {
					log.Info("removing stale aggregator", "addr", id)
					delete(initial, id)
					sm.RemoveSink(id)
				}
			}
		}
	}()
	return nil
}

type Sink interface {
	Info() *SinkInfo
	Connect() error
	Close()
	GetCursor(hostID string) (*utils.HostCursor, error)
	Write(m message) error
	Shutdown()
	ShutdownCh() chan struct{}
}

type LogAggregatorSink struct {
	sm *SinkManager

	id          string
	logger      log15.Logger
	addr        string
	hostManaged bool

	conn             net.Conn
	aggregatorClient *client.Client

	shutdownOnce sync.Once
	shutdownCh   chan struct{}
}

func NewLogAggregatorSink(sm *SinkManager, info *SinkInfo) (*LogAggregatorSink, error) {
	cfg := &ct.LogAggregatorSinkConfig{}
	if err := json.Unmarshal(info.Config, cfg); err != nil {
		return nil, err
	}
	return &LogAggregatorSink{
		sm:          sm,
		id:          info.ID,
		addr:        cfg.Addr,
		hostManaged: info.HostManaged,
		shutdownCh:  make(chan struct{}),
	}, nil
}

func (s *LogAggregatorSink) Info() *SinkInfo {
	config, _ := json.Marshal(ct.LogAggregatorSinkConfig{Addr: s.addr})
	return &SinkInfo{
		ID:          s.id,
		Kind:        ct.SinkKindLogaggregator,
		Config:      config,
		HostManaged: s.hostManaged,
	}
}

func (s *LogAggregatorSink) Connect() error {
	// Connect TCP connection to aggregator
	conn, err := dialer.Retry.Dial("tcp", s.addr)
	if err != nil {
		return err
	}
	// Connect to logaggregator HTTP endpoint
	host, _, _ := net.SplitHostPort(s.addr)
	c, err := client.New("http://" + host)
	if err != nil {
		conn.Close()
		return err
	}
	s.conn = conn
	s.aggregatorClient = c
	return nil
}

func (s *LogAggregatorSink) Close() {
	if s.conn != nil {
		s.conn.Close()
	}
}

func (s *LogAggregatorSink) GetCursor(hostID string) (*utils.HostCursor, error) {
	cursors, err := s.aggregatorClient.GetCursors()
	if err != nil {
		return nil, err
	}
	var aggCursor *utils.HostCursor
	if c, ok := cursors[hostID]; ok {
		aggCursor = &c
	}
	return aggCursor, nil
}

func (s *LogAggregatorSink) Write(m message) error {
	_, err := s.conn.Write(rfc6587.Bytes(m.Message))
	return err
}

func (s *LogAggregatorSink) Shutdown() {
	s.shutdownOnce.Do(func() { close(s.shutdownCh) })
}

func (s *LogAggregatorSink) ShutdownCh() chan struct{} {
	return s.shutdownCh
}

// SyslogSink is a flexible sink that can connect to TCP/TLS endpoints that use syslog framing.
// The prefix of the message can be customised using a template.
type SyslogSink struct {
	sm *SinkManager

	id             string
	url            string
	prefix         string
	useIDs         bool
	insecure       bool
	structuredData bool
	format         ct.SyslogFormat

	mtx          sync.RWMutex
	cache        *lru.Cache
	cursor       *utils.HostCursor
	template     *template.Template
	conn         net.Conn
	shutdownOnce sync.Once
	shutdownCh   chan struct{}
}

func NewSyslogSink(sm *SinkManager, info *SinkInfo) (sink *SyslogSink, err error) {
	cfg := &ct.SyslogSinkConfig{}
	if err := json.Unmarshal(info.Config, cfg); err != nil {
		return nil, err
	}
	var t *template.Template
	if cfg.Prefix != "" {
		t, err = template.New("").Parse(cfg.Prefix)
		if err != nil {
			return nil, err
		}
	}
	format := cfg.Format
	if format == "" {
		format = ct.SyslogFormatRFC6587
	}

	return &SyslogSink{
		sm:             sm,
		id:             info.ID,
		url:            cfg.URL,
		prefix:         cfg.Prefix,
		useIDs:         cfg.UseIDs,
		insecure:       cfg.Insecure,
		structuredData: cfg.StructuredData,
		format:         format,
		cache:          lru.New(1000),
		template:       t,
		cursor:         info.Cursor,
		shutdownCh:     make(chan struct{}),
	}, nil
}

func (s *SyslogSink) Info() *SinkInfo {
	s.mtx.RLock()
	defer s.mtx.RUnlock()
	config, _ := json.Marshal(ct.SyslogSinkConfig{URL: s.url, Prefix: s.prefix})
	return &SinkInfo{
		ID:     s.id,
		Kind:   ct.SinkKindSyslog,
		Config: config,
		Cursor: s.cursor,
	}
}

func (s *SyslogSink) Connect() error {
	u, err := url.Parse(s.url)
	if err != nil {
		return err
	}
	host, port, _ := net.SplitHostPort(u.Host)
	if port == "" {
		port = "514"
	}
	addr := net.JoinHostPort(host, port)
	var conn net.Conn
	switch u.Scheme {
	case "syslog":
		conn, err = net.Dial("tcp", addr)
	case "syslog+tls":
		tlsConfig := tlsconfig.SecureCiphers(&tls.Config{})
		if s.insecure {
			tlsConfig.InsecureSkipVerify = true
		}
		conn, err = tls.Dial("tcp", addr, tlsConfig)
	default:
		return fmt.Errorf("unknown protocol %s", u.Scheme)
	}
	if err != nil {
		return err
	}
	s.conn = conn
	return nil
}

func (s *SyslogSink) GetCursor(_ string) (*utils.HostCursor, error) {
	s.mtx.RLock()
	defer s.mtx.RUnlock()
	return s.cursor, nil
}

// Parses JobID and ProcType from header ProcID field
// Always returns JobID, returns ProcType if available
func parseProcID(procID []byte) (string, string) {
	procTypeID := strings.SplitN(string(procID), ".", 2)
	if len(procTypeID) == 2 {
		return procTypeID[1], procTypeID[0]
	}
	return procTypeID[0], ""
}

var msgSep = []byte{' '}

type cachedJob struct {
	AppName string
	Prefix  []byte
}

func (s *SyslogSink) Write(m message) error {
	// Lookup job in cache
	var appName string
	var prefix []byte
	if cached, ok := s.cache.Get(string(m.Message.ProcID)); ok {
		if c, ok := cached.(cachedJob); ok {
			appName = c.AppName
			prefix = c.Prefix
		}
	}

	var updateCache bool
	// Get the job name if the cache hasn't been populated yet
	var job *host.ActiveJob
	if appName == "" {
		jobID, _ := parseProcID(m.Message.ProcID)
		job = s.sm.state.GetJob(jobID)
		if job != nil && job.Job != nil {
			appName = job.Job.Metadata["flynn-controller.app_name"]
			updateCache = true
		}
	}

	// If not in the cache execute the template
	if s.template != nil && prefix == nil {
		if job != nil && job.Job != nil {
			var buf bytes.Buffer
			if err := s.template.Execute(&buf, job.Job); err != nil {
				return err
			}
			prefix = buf.Bytes()
			updateCache = true
		}
	}

	// Update the cache with app name and prefix
	if updateCache {
		s.cache.Add(string(m.Message.ProcID), cachedJob{
			AppName: appName,
			Prefix:  prefix,
		})
	}

	// Copy the message as we need to mutate it, this also drops structured data
	msg := rfc5424.NewMessage(&m.Message.Header, m.Message.Msg)
	if s.structuredData {
		msg.StructuredData = make([]byte, len(m.Message.StructuredData))
		copy(msg.StructuredData, m.Message.StructuredData)
	}

	// If the generated/cached prefix isn't 0 length then modify the message body
	if len(prefix) != 0 {
		msg.Msg = bytes.Join([][]byte{prefix, m.Message.Msg}, msgSep)
	}

	// Overwrite syslog APP_NAME with controller app name unless IDs are to be used
	if !s.useIDs {
		msg.AppName = []byte(appName)
	}

	// Write to the remote syslog
	var data []byte
	switch s.format {
	case ct.SyslogFormatRFC6587:
		data = rfc6587.Bytes(msg)
	case ct.SyslogFormatNewline:
		data = append(msg.Bytes(), '\n')
	}
	_, err := s.conn.Write(data)
	if err != nil {
		return err
	}

	// Cursor needs to be mutex protected to prevent race when persisting to disk
	s.mtx.Lock()
	defer s.mtx.Unlock()
	s.cursor = m.HostCursor
	return nil
}

func (s *SyslogSink) Close() {
	if s.conn != nil {
		s.conn.Close()
	}
}

func (s *SyslogSink) Shutdown() {
	s.shutdownOnce.Do(func() { close(s.shutdownCh) })
}

func (s *SyslogSink) ShutdownCh() chan struct{} {
	return s.shutdownCh
}
