package iotool

import (
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
	return s.W.Write(p)
}
