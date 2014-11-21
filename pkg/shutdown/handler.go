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

type Handler struct {
	Active bool

	mtx  sync.RWMutex
	done chan struct{}
}

func NewHandler() *Handler {
	h := &Handler{done: make(chan struct{})}
	go h.wait()
	return h
}

func BeforeExit(f func()) {
	NewHandler().BeforeExit(f)
}

func (h *Handler) BeforeExit(f func()) {
	h.mtx.RLock()
	go func() {
		<-h.done
		f()
		h.mtx.RUnlock()
	}()
}

func (h *Handler) Fatal(v ...interface{}) {
	h.exit(errors.New(fmt.Sprint(v...)))
}

func (h *Handler) wait() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, os.Signal(syscall.SIGTERM))
	<-ch
	h.exit(nil)
}

func (h *Handler) exit(err error) {
	h.Active = true
	// signal exit handlers
	close(h.done)
	// wait for exit handlers to finish
	h.mtx.Lock()
	if err != nil {
		log.New(os.Stderr, "", log.Lshortfile|log.Lmicroseconds).Output(2, err.Error())
		os.Exit(1)
	}
	os.Exit(0)
}
