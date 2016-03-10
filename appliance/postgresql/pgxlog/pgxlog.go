//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.
//
// This file is derived from:
// https://github.com/joyent/manatee-state-machine/blob/d441fe941faddb51d6e6237d792dd4d7fae64cc6/lib/xlog.js
//
// Copyright (c) 2014, Joyent, Inc.
// Copyright (c) 2015, Prime Directive, Inc.
//

/*

Package pgxlog provides constants and functions for working with PostgreSQL xlog positions.

The package makes a number of assumptions about the format of xlog positions.
It's not totally clear that this is a committed Postgres interface, but it seems
to be true.

We assume that postgres xlog positions are represented as strings of the form:

    filepart/offset		e.g., "0/17BB660"

where both "filepart" and "offset" are hexadecimal numbers. xlog position F1/O1
is at least as new as F2/O2 if (F1 > F2) or (F1 == F2 and O1 >= O2). We try to
avoid assuming that they're zero-padded (i.e., that a simple string comparison
might do the right thing). We also don't make any assumptions about the size of
each file, which means we can't compute the actual difference between two
positions.

*/

package pgxlog

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/flynn/flynn/pkg/sirenia/xlog"
)

const Zero xlog.Position = "0/00000000"

type PgXLog struct{}

func (p PgXLog) Zero() xlog.Position {
	return Zero
}

// Increment increments an xlog position by the given number.
func (p PgXLog) Increment(pos xlog.Position, increment int) (xlog.Position, error) {
	parts, err := parse(pos)
	if err != nil {
		return "", err
	}
	return makePosition(parts[0], parts[1]+increment), nil
}

// Compare compares two xlog positions returning -1 if xlog1 < xlog2, 0 if xlog1
// == xlog2, and 1 if xlog1 > xlog2.
func (p PgXLog) Compare(xlog1, xlog2 xlog.Position) (int, error) {
	p1, err := parse(xlog1)
	if err != nil {
		return 0, err
	}
	p2, err := parse(xlog2)
	if err != nil {
		return 0, err
	}

	if p1[0] == p2[0] && p1[1] == p2[1] {
		return 0, nil
	}
	if p1[0] > p2[0] || p1[0] == p2[0] && p1[1] > p2[1] {
		return 1, nil
	}
	return -1, nil
}

// parse takes an xlog position emitted by postgres and returns an array of two
// integers representing the filepart and offset components of the xlog
// position. This is an internal representation that should not be exposed
// outside of this package.
func parse(xlog xlog.Position) (res [2]int, err error) {
	parts := strings.SplitN(string(xlog), "/", 2)
	if len(parts) != 2 {
		err = fmt.Errorf("malformed xlog position %q", xlog)
		return
	}

	res[0], err = parseHex(parts[0])
	if err != nil {
		return
	}
	res[1], err = parseHex(parts[1])

	return
}

// MakePosition constructs an xlog position string from a numeric file part and
// offset.
func makePosition(filepart int, offset int) xlog.Position {
	return xlog.Position(fmt.Sprintf("%X/%08X", filepart, offset))
}

func parseHex(s string) (int, error) {
	res, err := strconv.ParseInt(s, 16, 64)
	return int(res), err
}
