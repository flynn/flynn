package sse

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"sync"
	"time"

	hh "github.com/flynn/flynn/pkg/httphelper"
	log "gopkg.in/inconshreveable/log15.v2"
)

type identifier interface {
	EventID() string
}

type Stream struct {
	once      sync.Once
	w         *writer
	rw        http.ResponseWriter
	fw        hh.FlushWriter
	ch        interface{}
	closeChan chan struct{}
	doneChan  chan struct{}
	closed    bool
	logger    log.Logger
	Done      chan struct{}
}

func NewStream(w http.ResponseWriter, ch interface{}, l log.Logger) *Stream {
	sw := newWriter(w)
	return &Stream{
		rw:        w,
		w:         sw,
		ch:        ch,
		closeChan: make(chan struct{}),
		doneChan:  make(chan struct{}),
		Done:      make(chan struct{}),
		logger:    l,
	}
}

func ServeStream(w http.ResponseWriter, ch interface{}, l log.Logger) {
	s := NewStream(w, ch, l)
	s.Serve()
	s.Wait()
}

func (s *Stream) Serve() {
	s.rw.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	s.rw.WriteHeader(200)
	s.w.Flush()

	s.fw = hh.FlushWriter{Writer: newWriter(s.rw), Enabled: true}

	if cw, ok := s.rw.(http.CloseNotifier); ok {
		ch := cw.CloseNotify()
		go func() {
			<-ch
			s.Close()
		}()
	}

	closeChanValue := reflect.ValueOf(s.closeChan)
	chValue := reflect.ValueOf(s.ch)
	go func() {
		defer s.done()
		for {
			chosen, v, ok := reflect.Select([]reflect.SelectCase{
				{
					Dir:  reflect.SelectRecv,
					Chan: closeChanValue,
				},
				{
					Dir:  reflect.SelectRecv,
					Chan: reflect.ValueOf(time.After(30 * time.Second)),
				},
				{
					Dir:  reflect.SelectRecv,
					Chan: chValue,
				},
			})
			switch chosen {
			case 0:
				return
			case 1:
				s.sendKeepAlive()
			default:
				if !ok {
					return
				}
				if err := s.send(v.Interface()); err != nil {
					s.Error(err)
					return
				}
			}
		}
	}()
}

func (s *Stream) Wait() {
	<-s.doneChan
}

func (s *Stream) done() {
	close(s.doneChan)
	close(s.Done)
	s.Close()
}

func (s *Stream) send(v interface{}) error {
	if i, ok := v.(identifier); ok {
		s.w.WriteID(i.EventID())
	}
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = s.fw.Write(data)
	return err
}

func (s *Stream) sendKeepAlive() error {
	if _, err := s.w.w.Write([]byte(":\n")); err != nil {
		return err
	}
	s.w.Flush()
	return nil
}

func (s *Stream) logError(err error) {
	if s.logger != nil {
		s.logger.Debug(err.Error())
	} else {
		fmt.Println(err)
	}
}

func (s *Stream) Error(err error) {
	if _, e := s.w.Error(err); e != nil {
		s.logError(err)
		s.logError(e)
	}
}

func (s *Stream) Close() {
	s.once.Do(func() {
		s.closed = true
		close(s.closeChan)
		s.Wait()
	})
}

func (s *Stream) CloseWithError(err error) {
	s.Close()
	s.Error(err)
}
