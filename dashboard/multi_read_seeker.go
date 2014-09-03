package main

// Taken from http://play.golang.org/p/BNL9QCnUHB

import (
	"io"
	"os"
)

type doubleReadSeeker struct {
	rs1, rs2       io.ReadSeeker
	rs1len, rs2len int64
	second         bool
	pos            int64
}

func (r *doubleReadSeeker) Seek(offset int64, whence int) (int64, error) {
	var err error
	switch whence {
	case os.SEEK_SET:
		if offset < r.rs1len {
			r.second = false
			r.pos, err = r.rs1.Seek(offset, os.SEEK_SET)
			return r.pos, err
		} else {
			r.second = true
			r.pos, err = r.rs2.Seek(offset-r.rs1len, os.SEEK_SET)
			r.pos += r.rs1len
			return r.pos, err
		}
	case os.SEEK_END: // negative offset
		return r.Seek(r.rs1len+r.rs2len+offset-1, os.SEEK_SET)
	default: // os.SEEK_CUR
		return r.Seek(r.pos+offset, os.SEEK_SET)
	}
}

func (r *doubleReadSeeker) Read(p []byte) (n int, err error) {
	switch {
	case r.pos >= r.rs1len: // read only from the second reader
		n, err := r.rs2.Read(p)
		r.pos += int64(n)
		return n, err
	case r.pos+int64(len(p)) <= r.rs1len: // read only from the first reader
		n, err := r.rs1.Read(p)
		r.pos += int64(n)
		return n, err
	default: // read on the border - end of first reader and start of second reader
		n1, err := r.rs1.Read(p)
		r.pos += int64(n1)
		if r.pos != r.rs1len || (err != nil && err != io.EOF) {
			// Read() might not read all, return
			// If error (but not EOF), return
			return n1, err
		}
		_, err = r.rs2.Seek(0, os.SEEK_SET)
		if err != nil {
			return n1, err
		}
		r.second = true
		n2, err := r.rs2.Read(p[n1:])
		r.pos += int64(n2)
		return n1 + n2, err
	}
}

func multiReadSeeker(rs []io.ReadSeeker, leftmost bool) (io.ReadSeeker, int64, error) {
	if len(rs) == 1 {
		r := rs[0]
		l, err := r.Seek(0, os.SEEK_END)
		if err != nil {
			return nil, 0, err
		}
		if leftmost {
			_, err = r.Seek(0, os.SEEK_SET)
		}
		return r, l, err
	} else {
		rs1, l1, err := multiReadSeeker(rs[:len(rs)/2], leftmost)
		if err != nil {
			return nil, 0, err
		}
		rs2, l2, err := multiReadSeeker(rs[len(rs)/2:], false)
		if err != nil {
			return nil, 0, err
		}
		return &doubleReadSeeker{rs1, rs2, l1, l2, false, 0}, l1 + l2, nil
	}
}

type emptyReadSeeker struct{}

func (r *emptyReadSeeker) Read(p []byte) (n int, err error) {
	return 0, io.EOF
}

func (r *emptyReadSeeker) Seek(offset int64, whence int) (int64, error) {
	return 0, io.EOF
}

// MultiReadSeeker returns a ReadSeeker that's the logical concatenation of the provided
// input readseekers. After calling this method the initial position is set to the
// beginning of the first ReadSeeker. At the end of a ReadSeeker, Read always advances
// to the beginning of the next ReadSeeker and returns EOF at the end of the last ReadSeeker.
// Seek can be used over the sum of lengths of all readseekers.
//
// When a MultiReadSeeker is used, no Read and Seek operations should be made on
// its ReadSeeker components and the length of the readseekers should not change.
// Also, users should make no assumption on the state of individual readseekers
// while the MultiReadSeeker is used.
func MultiReadSeeker(rs ...io.ReadSeeker) io.ReadSeeker {
	if len(rs) == 0 {
		return &emptyReadSeeker{}
	}
	r, _, err := multiReadSeeker(rs, true)
	if err != nil {
		return &emptyReadSeeker{}
	}
	return r
}
