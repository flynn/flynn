package shutdown

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
)

var h = newHandler()

type handler struct {
	active atomic.Value
	mtx    sync.Mutex
	stack  []func()
}

func newHandler() *handler {
	h := &handler{}
	h.active.Store(false)
	go h.wait()
	return h
}

func IsActive() bool {
	return h.active.Load().(bool)
}

func BeforeExit(f func()) {
	h.BeforeExit(f)
}

func (h *handler) BeforeExit(f func()) {
	h.mtx.Lock()
	h.stack = append(h.stack, f)
	h.mtx.Unlock()
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
	h.mtx.Lock()
	h.active.Store(true)
	for i := len(h.stack) - 1; i > 0; i-- {
		h.stack[i]()
	}
	if err != nil {
		log.New(os.Stderr, "", log.Lshortfile|log.Lmicroseconds).Output(3, err.Error())
		os.Exit(1)
	}
	os.Exit(0)
}
