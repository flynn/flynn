package sparse

import (
	"bytes"
	"fmt"
	"os"

	log "github.com/Sirupsen/logrus"
)

// Interval [Begin, End) is non-inclusive at the End
type Interval struct {
	Begin, End int64
}

// Len returns length of Interval
func (interval Interval) Len() int64 {
	return interval.End - interval.Begin
}

// String conversion
func (interval Interval) String() string {
	if interval.Begin%Blocks == 0 && interval.End%Blocks == 0 {
		return fmt.Sprintf("[%8d:%8d](%3d)", interval.Begin/Blocks, interval.End/Blocks, interval.Len()/Blocks)
	}
	return fmt.Sprintf("{unaligned}[%8d:%8d](%3d)", interval.Begin, interval.End, interval.Len())
}

// FileIntervalKind distinguishes between data and hole
type FileIntervalKind int

// Sparse file Interval types
const (
	SparseData FileIntervalKind = 1 + iota
	SparseHole
	SparseIgnore // ignore file interval (equal src vs dst part)
)

// FileInterval describes either sparse data Interval or a hole
type FileInterval struct {
	Kind FileIntervalKind
	Interval
}

func (i FileInterval) String() string {
	kind := "?"
	switch i.Kind {
	case SparseData:
		kind = "D"
	case SparseHole:
		kind = " "
	case SparseIgnore:
		kind = "i"
	}
	return fmt.Sprintf("%s%v", kind, i.Interval)
}

const (
	// Blocks : block size in bytes
	Blocks int64 = 4 << 10 // 4k
)

// os.Seek sparse whence values.
const (
	// Adjust the file offset to the next location in the file
	// greater than or equal to offset containing data.  If offset
	// points to data, then the file offset is set to offset.
	seekData int = 3

	// Adjust the file offset to the next hole in the file greater
	// than or equal to offset.  If offset points into the middle of
	// a hole, then the file offset is set to offset.  If there is no
	// hole past offset, then the file offset is adjusted to the End
	// of the file (i.e., there is an implicit hole at the End of any
	// file).
	seekHole int = 4
)

// syscall.Fallocate mode bits
const (
	// default is extend size
	fallocFlKeepSize uint32 = 1

	// de-allocates range
	fallocFlPunchHole uint32 = 2
)

func MakeData(interval FileInterval) []byte {
	data := make([]byte, interval.Len())
	if SparseData == interval.Kind {
		for i := range data {
			data[i] = byte(interval.Begin/Blocks + 1)
		}
	}
	return data
}

func isHashDifferent(a, b []byte) bool {
	return !bytes.Equal(a, b)
}

// RetrieveLayoutStream streams sparse file data/hole layout
// Based on fiemap
// To abort: abortStream <- error
// Check status: err := <- errStream
// Usage: go RetrieveLayoutStream(...)
func RetrieveLayoutStream(abortStream <-chan error, file *os.File, r Interval, layoutStream chan<- FileInterval, errStream chan<- error) {
	const extents = 1024
	const chunkSizeMax = 1 /*GB*/ << 30
	chunkSize := r.Len()
	if chunkSize > chunkSizeMax {
		chunkSize = chunkSizeMax
	}

	chunk := Interval{r.Begin, r.Begin + chunkSize}
	// Process file extents for each chunk
	intervalLast := Interval{chunk.Begin, chunk.Begin}
	for chunk.Begin < r.End {
		if chunk.End > r.End {
			chunk.End = r.End
		}

		for more := true; more && chunk.Len() > 0; {
			fiemap := NewFiemapFile(file)
			_, ext, errno := fiemap.Fiemap(uint32(chunk.Len()))
			if errno != 0 {
				close(layoutStream)
				errStream <- &os.PathError{Op: "Fiemap", Path: file.Name(), Err: errno}
				return
			}
			if len(ext) == 0 {
				break
			}

			// Process each extent
			for _, e := range ext {
				interval := Interval{int64(e.Logical), int64(e.Logical + e.Length)}
				log.Debug("Extent:", interval, e.Flags)
				if e.Flags&FIEMAP_EXTENT_LAST != 0 {
					more = false
				}
				if intervalLast.End < interval.Begin {
					if intervalLast.Len() > 0 {
						// Pop last Data
						layoutStream <- FileInterval{SparseData, intervalLast}
					}
					// report hole
					intervalLast = Interval{intervalLast.End, interval.Begin}
					layoutStream <- FileInterval{SparseHole, intervalLast}

					// Start data
					intervalLast = interval
				} else {
					// coalesce
					intervalLast.End = interval.End
				}
				chunk.Begin = interval.End
			}
		}
		chunk = Interval{chunk.End, chunk.End + chunkSize}
	}

	if intervalLast.Len() > 0 {
		// Pop last Data
		if intervalLast.End > r.End {
			intervalLast.End = r.End
		}
		layoutStream <- FileInterval{SparseData, intervalLast}
	}
	if intervalLast.End < r.End {
		// report hole
		layoutStream <- FileInterval{SparseHole, Interval{intervalLast.End, r.End}}
	}

	close(layoutStream)
	errStream <- nil
	return
}

// RetrieveLayout retrieves sparse file hole and data layout
func RetrieveLayout(file *os.File, r Interval) ([]FileInterval, error) {
	layout := make([]FileInterval, 0, 1024)
	abortStream := make(chan error)
	layoutStream := make(chan FileInterval, 128)
	errStream := make(chan error)

	go RetrieveLayoutStream(abortStream, file, r, layoutStream, errStream)
	for interval := range layoutStream {
		layout = append(layout, interval)
	}
	return layout, <-errStream
}
