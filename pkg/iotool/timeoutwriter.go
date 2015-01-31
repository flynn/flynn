package iotool

import (
	"fmt"
	"io"
	"sync"
	"time"
)

/*
	TimeoutWriter accepts input until expiration, then returns errors.

	An error will be written to the wrapped writer unless `Finished` is called
	before the timeout.
*/
type TimeoutWriter struct {
	w    io.Writer
	mtx  sync.Mutex
	done bool
}

var TimeoutErr = fmt.Errorf("writer closed due to timeout")

func NewTimeoutWriter(w io.Writer, patience time.Duration) *TimeoutWriter {
	return NewTimeoutWriterFromChan(w, time.After(patience))
}

func NewTimeoutWriterFromChan(w io.Writer, done <-chan time.Time) *TimeoutWriter {
	t := &TimeoutWriter{w: w}
	go func() {
		<-done
		t.mtx.Lock()
		defer t.mtx.Unlock()
		t.done = true
		if w != nil {
			fmt.Fprintln(w, TimeoutErr)
		}
	}()
	return t
}

func (w *TimeoutWriter) Write(p []byte) (int, error) {
	w.mtx.Lock()
	defer w.mtx.Unlock()
	if w.done {
		return 0, TimeoutErr
	}
	return w.w.Write(p)
}

func (w *TimeoutWriter) Finished() {
	w.mtx.Lock()
	defer w.mtx.Unlock()
	w.done = true
	w.w = nil
}
