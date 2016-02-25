package mdbxlog

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/flynn/flynn/pkg/sirenia/xlog"
)

// XLog implements a string serializable, comparable transaction log position.
type MDBXLog struct{}

// Zero Returns the zero position for this xlog
func (m MDBXLog) Zero() xlog.Position { return "" }

// Compare compares two xlog positions returning -1 if xlog1 < xlog2,
// 0 if xlog1 == xlog2, and 1 if xlog1 > xlog2.
func (m MDBXLog) Compare(xlog1, xlog2 xlog.Position) (int, error) {
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
	parts := strings.SplitN(string(x), "-", 3)
	if len(parts) != 3 {
		err = fmt.Errorf("malformed xlog position %q", x)
		return
	}

	pos, err = strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return 0, err
	}

	return pos, err
}
