package sparse

import "fmt"

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
