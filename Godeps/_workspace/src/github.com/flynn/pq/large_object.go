package pq

import (
	"fmt"
	"io"
	"sync"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-sql"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/pq/oid"
)

type LargeObjects struct {
	Has64 bool
	mtx   sync.Locker
	fp    *fastpath
}

const largeObjectFns = `select proname, oid from pg_catalog.pg_proc 
where proname in (
'lo_open', 
'lo_close', 
'lo_create', 
'lo_unlink', 
'lo_lseek', 
'lo_lseek64', 
'lo_tell', 
'lo_tell64', 
'lo_truncate', 
'lo_truncate64', 
'loread', 
'lowrite') 
and pronamespace = (select oid from pg_catalog.pg_namespace where nspname = 'pg_catalog')`

func NewLargeObjects(tx *sql.Tx) (*LargeObjects, error) {
	c, err := tx.Conn()
	if err != nil {
		return nil, err
	}

	c.Lock()
	cn, ok := c.Conn.(*conn)
	if !ok {
		c.Unlock()
		return nil, fmt.Errorf("pq: Expected driver.Conn to be *pq.conn, got %T", c.Conn)
	}
	if cn.fp == nil {
		cn.fp = newFastpath(cn)
	}
	if _, exists := cn.fp.fns["lo_open"]; !exists {
		c.Unlock()
		res, err := tx.Query(largeObjectFns)
		if err != nil {
			return nil, err
		}
		if err := cn.fp.addFunctions(res); err != nil {
			return nil, err
		}
		c.Lock()
	}

	lo := &LargeObjects{
		mtx: c.Locker,
		fp:  cn.fp,
	}
	_, lo.Has64 = cn.fp.fns["lo_lseek64"]

	c.Unlock()
	return lo, nil
}

type LargeObjectMode int32

const (
	LargeObjectModeWrite LargeObjectMode = 0x20000
	LargeObjectModeRead  LargeObjectMode = 0x40000
)

func (o *LargeObjects) Create(id oid.Oid) (oid.Oid, error) {
	o.mtx.Lock()
	defer o.mtx.Unlock()

	newOid, err := fpInt32(o.fp.CallFn("lo_create", []fpArg{fpIntArg(int32(id))}))
	return oid.Oid(newOid), err
}

func (o *LargeObjects) Open(oid oid.Oid, mode LargeObjectMode) (*LargeObject, error) {
	o.mtx.Lock()
	defer o.mtx.Unlock()

	fd, err := fpInt32(o.fp.CallFn("lo_open", []fpArg{fpIntArg(int32(oid)), fpIntArg(int32(mode))}))
	return &LargeObject{fd: fd, lo: o}, err
}

func (o *LargeObjects) Unlink(oid oid.Oid) error {
	o.mtx.Lock()
	defer o.mtx.Unlock()

	_, err := o.fp.CallFn("lo_unlink", []fpArg{fpIntArg(int32(oid))})
	return err
}

type LargeObject struct {
	fd int32
	lo *LargeObjects
}

func (o *LargeObject) Write(p []byte) (int, error) {
	o.lo.mtx.Lock()
	defer o.lo.mtx.Unlock()

	n, err := fpInt32(o.lo.fp.CallFn("lowrite", []fpArg{fpIntArg(o.fd), p}))
	return int(n), err
}

func (o *LargeObject) Read(p []byte) (int, error) {
	o.lo.mtx.Lock()
	defer o.lo.mtx.Unlock()

	res, err := o.lo.fp.CallFn("loread", []fpArg{fpIntArg(o.fd), fpIntArg(int32(len(p)))})
	if len(res) < len(p) {
		err = io.EOF
	}
	return copy(p, res), err
}

func (o *LargeObject) Seek(offset int64, whence int) (n int64, err error) {
	o.lo.mtx.Lock()
	defer o.lo.mtx.Unlock()

	if o.lo.Has64 {
		n, err = fpInt64(o.lo.fp.CallFn("lo_lseek64", []fpArg{fpIntArg(o.fd), fpInt64Arg(offset), fpIntArg(int32(whence))}))
	} else {
		var n32 int32
		n32, err = fpInt32(o.lo.fp.CallFn("lo_lseek", []fpArg{fpIntArg(o.fd), fpIntArg(int32(offset)), fpIntArg(int32(whence))}))
		n = int64(n32)
	}
	return
}

func (o *LargeObject) Tell() (n int64, err error) {
	o.lo.mtx.Lock()
	defer o.lo.mtx.Unlock()

	if o.lo.Has64 {
		n, err = fpInt64(o.lo.fp.CallFn("lo_tell64", []fpArg{fpIntArg(o.fd)}))
	} else {
		var n32 int32
		n32, err = fpInt32(o.lo.fp.CallFn("lo_tell", []fpArg{fpIntArg(o.fd)}))
		n = int64(n32)
	}
	return
}

func (o *LargeObject) Truncate(size int64) (err error) {
	o.lo.mtx.Lock()
	defer o.lo.mtx.Unlock()

	if o.lo.Has64 {
		_, err = o.lo.fp.CallFn("lo_truncate64", []fpArg{fpIntArg(o.fd), fpInt64Arg(size)})
	} else {
		_, err = o.lo.fp.CallFn("lo_truncate", []fpArg{fpIntArg(o.fd), fpIntArg(int32(size))})
	}
	return
}

func (o *LargeObject) Close() error {
	o.lo.mtx.Lock()
	defer o.lo.mtx.Unlock()

	_, err := o.lo.fp.CallFn("lo_close", []fpArg{fpIntArg(o.fd)})
	return err
}
