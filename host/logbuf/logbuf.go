package logbuf

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/lumberjack"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/tysontate/gommap"
)

func NewLog(l *lumberjack.Logger) *Log {
	if l == nil {
		l = &lumberjack.Logger{}
	}
	if l.MaxSize == 0 {
		l.MaxSize = 100 * lumberjack.Megabyte
	}
	log := &Log{l: l, files: make(map[string]*file)}
	log.changed.L = log.mtx.RLocker()
	return log
}

type Log struct {
	l *lumberjack.Logger

	changed sync.Cond
	mtx     sync.RWMutex
	name    string
	size    int64
	closed  bool

	filesMtx sync.Mutex
	files    map[string]*file
}

type Data struct {
	Stream    int      `json:"s"`
	Timestamp UnixTime `json:"t"`
	Message   string   `json:"m"`
}

type UnixTime struct{ time.Time }

func (t UnixTime) MarshalJSON() ([]byte, error) {
	return strconv.AppendInt(nil, t.UnixNano()/int64(time.Millisecond), 10), nil
}

func (t *UnixTime) UnmarshalJSON(data []byte) error {
	i, err := strconv.ParseInt(string(data), 10, 64)
	if err != nil {
		return errors.New("logbuf: invalid timestamp")
	}
	t.Time = time.Unix(0, i*int64(time.Millisecond))
	return nil
}

func (l *Log) ReadFrom(stream int, r io.Reader) error {
	j := json.NewEncoder(l.l)
	data := &Data{Stream: stream}

	buf := make([]byte, 32*1024)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			data.Timestamp = UnixTime{time.Now()}
			data.Message = string(buf[:n])
			if err := j.Encode(data); err != nil {
				return err
			}
			l.mtx.Lock()
			l.name, l.size = l.l.File()
			l.changed.Broadcast()
			l.mtx.Unlock()
		}
		if err != nil {
			if err == io.EOF {
				err = nil
			}
			return err
		}
	}
}

func (l *Log) Close() error {
	l.mtx.Lock()
	l.closed = true
	l.changed.Broadcast()
	l.mtx.Unlock()
	return l.l.Close()
}

func (l *Log) NewReader() *Reader {
	return &Reader{l: l}
}

func (l *Log) openFile(name string, size int64) (*file, error) {
	if size == 0 {
		size = l.l.MaxSize
	}

	l.filesMtx.Lock()
	defer l.filesMtx.Unlock()

	f, ok := l.files[filepath.Base(name)]
	if !ok {
		fd, err := os.Open(name)
		if err != nil {
			return nil, err
		}
		f = &file{name: filepath.Base(name), l: l}
		f.data, err = gommap.MapRegion(fd.Fd(), 0, size, gommap.PROT_READ, gommap.MAP_SHARED)
		if err != nil {
			return nil, err
		}
		fd.Close()
		l.files[f.name] = f
	}
	f.addRef()

	return f, nil
}

func (l *Log) closeFile(name string) {
	l.filesMtx.Lock()
	delete(l.files, name)
	l.filesMtx.Unlock()
}

type file struct {
	name string
	data gommap.MMap
	l    *Log

	mtx  sync.Mutex
	refs int
}

func (f *file) Close() error {
	f.mtx.Lock()
	defer f.mtx.Unlock()
	f.refs--
	if f.refs <= 0 {
		f.l.closeFile(f.name)
		f.data.UnsafeUnmap()
	}
	return nil
}

func (f *file) addRef() {
	f.mtx.Lock()
	f.refs++
	f.mtx.Unlock()
}

type Reader struct {
	l *Log
	f *file
	d *jsonDecoder
}

func (r *Reader) Close() error {
	if r.f == nil {
		return nil
	}
	return r.f.Close()
}

func (r *Reader) SeekToEnd() error {
	r.l.mtx.RLock()
	name, size := r.l.name, r.l.size
	r.l.mtx.RUnlock()
	if name == "" {
		return nil
	}
	var err error
	if r.f == nil || r.f.name != name {
		if r.f != nil {
			r.f.Close()
		}
		r.f, err = r.l.openFile(name, 0)
	}
	r.d = &jsonDecoder{f: r.f, pos: int(size)}
	return err
}

func (r *Reader) ReadData(blocking bool) (*Data, error) {
	if r.f == nil {
		if err := r.openNextFile(); err != nil {
			if blocking && err == io.EOF {
				// wait for a file to be opened and retry
				r.l.mtx.RLock()
				r.l.changed.Wait()
				r.l.mtx.RUnlock()
				return r.ReadData(blocking)
			}
			return nil, err
		}
	}
	data := &Data{}
	if err := r.d.Decode(data); err == nil {
		return data, nil
	} else if err != io.EOF {
		return nil, err
	}
	if err := r.openNextFile(); err == errLastFile {
		// r.l.mtx was left RLocked, unlock it or wait for a change if blocking
		if !blocking {
			r.l.mtx.RUnlock()
			return nil, io.EOF
		}
		r.l.changed.Wait()
		r.l.mtx.RUnlock()
	} else if err != nil {
		return nil, err
	}
	return r.ReadData(blocking)
}

var errLastFile = errors.New("current file is the most recent")

func (r *Reader) openNextFile() error {
	r.l.mtx.RLock()
	if r.f != nil && r.f.name == filepath.Base(r.l.name) {
		if r.l.closed {
			r.l.mtx.RUnlock()
			return io.EOF
		}
		// intentially leave r.l.mtx locked, it will be RUnlocked in the caller
		// by a call to r.l.changed.Wait()
		return errLastFile
	}
	r.l.mtx.RUnlock()

	// TODO: investigate if this can race and list files that don't exist
	// when logs are going too fast
	var fi os.FileInfo
	files := r.l.l.OldFiles()
	for i, f := range files {
		if r.f != nil && f.Name() == r.f.name {
			if i < len(files)-1 {
				fi = files[i+1]
			}
			break
		}
	}
	r.l.mtx.RLock()
	name := r.l.name
	r.l.mtx.RUnlock()
	if name == "" && fi == nil && len(files) > 0 {
		fi = files[0]
	}
	if r.f != nil {
		r.f.Close()
	}
	var err error
	if fi != nil {
		r.f, err = r.l.openFile(filepath.Join(r.l.l.Dir, fi.Name()), fi.Size())
	} else {
		if name == "" {
			return io.EOF
		}
		r.f, err = r.l.openFile(name, 0)
	}
	r.d = &jsonDecoder{f: r.f}
	return err
}

type jsonDecoder struct {
	f   *file
	pos int
}

func (d *jsonDecoder) Decode(v interface{}) error {
	if d.pos >= len(d.f.data) {
		return io.EOF
	}
	var end int
outer:
	for i := range d.f.data[d.pos:] {
		end = d.pos + i
		switch d.f.data[end] {
		case '\n':
			end++
			break outer
		case 0:
			break outer
		}
	}
	if d.pos == end {
		return io.EOF
	}
	err := json.Unmarshal(d.f.data[d.pos:end], v)
	d.pos = end
	return err
}
