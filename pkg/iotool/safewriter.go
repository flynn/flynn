package iotool

import (
	"errors"
	"io"
	"sync"
)

// SafeWriter provides a thread-safe io.Writer
type SafeWriter struct {
	W   io.Writer
	mtx sync.Mutex
}

func (s *SafeWriter) Write(p []byte) (int, error) {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	if s.W == nil {
		return 0, errors.New("writes disabled")
	}
	return s.W.Write(p)
}

func (s *SafeWriter) SetWriter(w io.Writer) {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	s.W = w
}
