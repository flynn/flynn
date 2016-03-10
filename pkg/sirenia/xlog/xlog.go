package xlog

type Position string

type XLog interface {
	// Returns the zero position for this xlog
	Zero() Position
	// Compare compares two xlog positions returning -1 if xlog1 < xlog2, 0 if xlog1
	// == xlog2, and 1 if xlog1 > xlog2.
	Compare(Position, Position) (int, error)
}
