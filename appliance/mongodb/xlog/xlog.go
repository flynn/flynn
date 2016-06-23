package xlog

import (
	"strconv"

	"github.com/flynn/flynn/pkg/sirenia/xlog"
)

// XLog implements a string serializable, comparable transaction log position.
type XLog struct{}

// Zero Returns the zero position for this xlog
func (m XLog) Zero() xlog.Position { return "" }

// Compare compares two xlog positions returning -1 if xlog1 < xlog2,
// 0 if xlog1 == xlog2, and 1 if xlog1 > xlog2.
func (m XLog) Compare(xlog1, xlog2 xlog.Position) (int, error) {
	if xlog1 == xlog2 {
		return 0, nil
	}
	pos1, err := parseXlog(xlog1)
	if err != nil {
		return 0, err
	}
	pos2, err := parseXlog(xlog2)
	if err != nil {
		return 0, err
	}
	if pos1 > pos2 {
		return 1, nil
	}
	return -1, nil
}

// parseXlog parses a string xlog position into an int64
// Returns an error if the xlog position is not formatted correctly.
func parseXlog(x xlog.Position) (pos int64, err error) {
	if x == "" {
		return 0, nil
	}
	pos, err = strconv.ParseInt(string(x), 10, 64)
	if err != nil {
		return 0, err
	}

	return pos, err
}
