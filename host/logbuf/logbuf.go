package logbuf

import (
	"encoding/json"
	"errors"
	"io"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/ActiveState/tail"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/natefinch/lumberjack"
	"github.com/flynn/flynn/pkg/random"
)

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

func NewLog(l *lumberjack.Logger) *Log {
	if l == nil {
		l = &lumberjack.Logger{}
	}
	if l.MaxSize == 0 {
		l.MaxSize = 100 // megabytes
	}
	if l.Filename == "" {
		l.Filename = path.Join(os.TempDir(), random.String(16)+".log")
	}
	l.Rotate() // force creating a log file straight away
	log := &Log{
		l:      l,
		closed: make(chan struct{}),
	}
	return log
}

type Log struct {
	l      *lumberjack.Logger
	closed chan struct{}
}

// Watch stream for new log events and transmit them.
func (l *Log) Follow(stream int, r io.Reader) error {
	data := Data{Stream: stream}
	buf := make([]byte, 32*1024)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			data.Timestamp = UnixTime{time.Now()}
			data.Message = string(buf[:n])

			l.Write(data)
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// Write a log event to the logfile.
func (l *Log) Write(data Data) error {
	return json.NewEncoder(l.l).Encode(data)
}

// Read old log lines from a logfile.
func (l *Log) Read(lines int, follow bool, ch chan Data, done chan struct{}) error {
	name := l.l.Filename

	var seek int64
	if lines == 0 {
		f, err := os.Open(name)
		defer f.Close()
		if err != nil {
			return err
		}
		if seek, err = f.Seek(0, os.SEEK_END); err != nil {
			return err
		}
	} else if lines == -1 {
		// return all lines
		dir := filepath.Dir(name)
		files, err := ioutil.ReadDir(dir)
		if err != nil {
			return err
		}
		basename := filepath.Base(name)
		ext := filepath.Ext(basename)
		id := strings.TrimSuffix(basename, ext)
		for _, f := range files {
			if !(strings.HasPrefix(f.Name(), id+"-") && strings.HasSuffix(f.Name(), ext)) {
				continue
			}

			t, err := tail.TailFile(filepath.Join(dir, f.Name()), tail.Config{
				Logger: tail.DiscardingLogger,
			})
			if err != nil {
				return err
			}
			for line := range t.Lines {
				data := Data{}
				if err := json.Unmarshal([]byte(line.Text), &data); err != nil {
					return err
				}
				ch <- data
			}
		}
	}

	t, err := tail.TailFile(name, tail.Config{
		Follow: follow,
		ReOpen: follow,
		Logger: tail.DiscardingLogger,
		Location: &tail.SeekInfo{
			Offset: seek,
			Whence: os.SEEK_SET,
		},
	})
	if err != nil {
		return err
	}
outer:
	for {
		select {
		case line, ok := <-t.Lines:
			if !ok {
				break outer
			}
			data := Data{}
			if err := json.Unmarshal([]byte(line.Text), &data); err != nil {
				return err
			}
			ch <- data
		case <-done:
			break outer
		case <-l.closed:
			break outer
		}
	}
	close(ch) // send a close event so we know everything was read
	return nil
}

func (l *Log) Close() error {
	close(l.closed)
	return l.l.Close()
}
