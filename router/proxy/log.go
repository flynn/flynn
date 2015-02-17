package proxy

import (
	"io"
	"sync/atomic"
)

// countingReadCloser is wrapper of io.ReadCloser that keeps track of the number
// of bytes read from it.
type countingReadCloser struct {
	io.ReadCloser
	count uint64
}

func (r *countingReadCloser) Read(b []byte) (int, error) {
	count, err := r.ReadCloser.Read(b)
	atomic.AddUint64(&r.count, uint64(count))
	return count, err
}
