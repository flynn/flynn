package pq

import (
	"encoding/binary"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-sql"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/pq/oid"
)

type fastpathArg []byte

func newFastpath(cn *conn) *fastpath {
	return &fastpath{cn: cn, fns: make(map[string]oid.Oid)}
}

type fastpath struct {
	cn  *conn
	fns map[string]oid.Oid
}

func (f *fastpath) functionOID(name string) oid.Oid {
	return f.fns[name]
}

func (f *fastpath) addFunction(name string, oid oid.Oid) {
	f.fns[name] = oid
}

func (f *fastpath) addFunctions(rows *sql.Rows) error {
	for rows.Next() {
		var name string
		var oid oid.Oid
		if err := rows.Scan(&name, &oid); err != nil {
			return err
		}
		f.addFunction(name, oid)
	}
	return rows.Err()
}

type fpArg []byte

func fpIntArg(n int32) fpArg {
	res := make([]byte, 4)
	binary.BigEndian.PutUint32(res, uint32(n))
	return res
}

func fpInt64Arg(n int64) fpArg {
	res := make([]byte, 8)
	binary.BigEndian.PutUint64(res, uint64(n))
	return res
}

func (f *fastpath) Call(oid oid.Oid, args []fpArg) (res []byte, err error) {
	defer f.cn.errRecover(&err)

	req := f.cn.writeBuf('F') // function call
	req.int32(int(oid))       // function object id
	req.int16(1)              // # of argument format codes
	req.int16(1)              // format code: binary
	req.int16(len(args))      // # of arguments
	for _, arg := range args {
		req.int32(len(arg)) // length of argument
		req.bytes(arg)      // argument value
	}
	req.int16(1) // response format code (binary)

	f.cn.send(req)

	for {
		t, r := f.cn.recv1()
		switch t {
		case 'V': // FunctionCallResponse
			data := r.next(r.int32())
			res = make([]byte, len(data))
			copy(res, data)
		case 'Z': // Ready for query
			f.cn.processReadyForQuery(r)
			// done
			return
		case 'E': // Error
			err = parseError(r)
		case 'N': // Notice
			// ignore
		default:
			errorf("unknown response for function call: %q", t)
		}
	}
}

func (f *fastpath) CallFn(fn string, args []fpArg) ([]byte, error) {
	return f.Call(f.functionOID(fn), args)
}

func fpInt32(data []byte, err error) (int32, error) {
	if err != nil {
		return 0, err
	}
	n := int32(binary.BigEndian.Uint32(data))
	return n, nil
}

func fpInt64(data []byte, err error) (int64, error) {
	if err != nil {
		return 0, err
	}
	return int64(binary.BigEndian.Uint64(data)), nil
}
