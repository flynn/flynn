package shutdown

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
)

var h = newHandler()

type handler struct {
	Active bool

	mtx  sync.RWMutex
	done chan struct{}
}

func newHandler() *handler {
	h := &handler{done: make(chan struct{})}
	go h.wait()
	return h
}

func IsActive() bool {
	return h.Active
}

func BeforeExit(f func()) {
	h.BeforeExit(f)
}

func (h *handler) BeforeExit(f func()) {
	h.mtx.RLock()
	go func() {
		<-h.done
		f()
		h.mtx.RUnlock()
	}()
}

func Fatal(v ...interface{}) {
	h.Fatal(v)
}

func (h *handler) Fatal(v ...interface{}) {
	h.exit(errors.New(fmt.Sprint(v...)))
}

func (h *handler) wait() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, os.Signal(syscall.SIGTERM))
	<-ch
	h.exit(nil)
}

func (h *handler) exit(err error) {
	h.Active = true
	// signal exit handlers
	close(h.done)
	// wait for exit handlers to finish
	h.mtx.Lock()
	if err != nil {
		log.New(os.Stderr, "", log.Lshortfile|log.Lmicroseconds).Output(3, err.Error())
		os.Exit(1)
	}
	os.Exit(0)
}
