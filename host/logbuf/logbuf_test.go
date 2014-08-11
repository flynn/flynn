package logbuf

import (
	"io"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/lumberjack"
	. "github.com/flynn/flynn/Godeps/_workspace/src/gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type S struct{}

var _ = Suite(&S{})

func (s *S) TestLogWriteRead(c *C) {
	stdoutR, stdoutW := io.Pipe()
	stderrR, stderrW := io.Pipe()
	defer stdoutW.Close()
	defer stderrW.Close()

	l := NewLog(&lumberjack.Logger{Dir: c.MkDir()})
	defer l.Close()
	r := l.NewReader()
	defer r.Close()
	_, err := r.ReadData(false)
	c.Assert(err, Equals, io.EOF)

	readFrom := func(stream int, r io.Reader) {
		if err := l.ReadFrom(stream, r); err != nil && err != io.EOF {
			c.Error(err)
		}
	}
	go readFrom(0, stdoutR)
	go readFrom(1, stderrR)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		stdoutW.Write([]byte("1"))
		stdoutW.Write([]byte("2"))
		wg.Done()
	}()
	go func() {
		stderrW.Write([]byte("3"))
		stderrW.Write([]byte("4"))
		wg.Done()
	}()
	wg.Wait()

	stdout, stderr := 0, 2
	for i := 0; i < 4; i++ {
		line, err := r.ReadData(false)
		c.Assert(err, IsNil)
		c.Assert(line.Timestamp.After(time.Now().Add(-time.Minute)), Equals, true)
		switch line.Stream {
		case 0:
			stdout++
			c.Assert(line.Message, Equals, strconv.Itoa(stdout))
		case 1:
			stderr++
			c.Assert(line.Message, Equals, strconv.Itoa(stderr))
		default:
			c.Errorf("unknown stream: %#v", line)
		}
	}
	_, err = r.ReadData(false)
	c.Assert(err, Equals, io.EOF)

	err = l.l.Rotate()
	c.Assert(err, IsNil)

	stdoutW.Write([]byte("5"))
	line, err := r.ReadData(false)
	c.Assert(err, IsNil)
	c.Assert(line.Message, Equals, "5")

	_, err = r.ReadData(false)
	c.Assert(err, Equals, io.EOF)
}

func (s *S) TestClosedRead(c *C) {
	l := NewLog(&lumberjack.Logger{Dir: c.MkDir()})
	pipeR, pipeW := io.Pipe()
	defer pipeW.Close()
	defer l.Close()
	go l.ReadFrom(0, pipeR)

	pipeW.Write([]byte("1"))

	r := l.NewReader()
	defer r.Close()
	line, err := r.ReadData(false)
	c.Assert(err, IsNil)
	c.Assert(line.Message, Equals, "1")

	_, err = r.ReadData(false)
	c.Assert(err, Equals, io.EOF)
}

func (s *S) TestBlockingRead(c *C) {
	l := NewLog(&lumberjack.Logger{Dir: c.MkDir()})
	pipeR, pipeW := io.Pipe()
	go l.ReadFrom(0, pipeR)

	ch := make(chan struct{})
	r := l.NewReader()
	defer r.Close()
	var data *Data
	readData := func() {
		var err error
		data, err = r.ReadData(true)
		c.Assert(err, IsNil)
		ch <- struct{}{}
	}
	waitData := func() {
		select {
		case <-ch:
		case <-time.After(time.Second):
			c.Error("timed out waiting for readData")
		}
	}

	for i := 0; i < 3; i++ {
		go readData()
		s := strconv.Itoa(i)
		pipeW.Write([]byte(s))
		waitData()
		c.Assert(data, Not(IsNil))
		c.Assert(data.Message, Equals, s)
		if i == 1 {
			l.l.Rotate()
		}
	}

	go func() {
		_, err := r.ReadData(true)
		c.Assert(err, Equals, io.EOF)
		ch <- struct{}{}
	}()

	runtime.Gosched()
	pipeW.Close()
	l.Close()
	<-ch
}

func (s *S) TestSeekToEnd(c *C) {
	l := NewLog(&lumberjack.Logger{Dir: c.MkDir()})
	defer l.Close()

	r := l.NewReader()
	defer r.Close()

	err := r.SeekToEnd()
	c.Assert(err, IsNil)

	data, err := r.ReadData(false)
	c.Assert(err, Equals, io.EOF)

	go func() {
		runtime.Gosched()
		l.ReadFrom(0, strings.NewReader("1"))
	}()
	data, err = r.ReadData(true)
	c.Assert(err, IsNil)
	c.Assert(data.Message, Equals, "1")

	l.ReadFrom(0, strings.NewReader("2"))
	err = r.SeekToEnd()
	c.Assert(err, IsNil)
	go l.ReadFrom(0, strings.NewReader("3"))
	data, err = r.ReadData(true)
	c.Assert(err, IsNil)
	c.Assert(data.Message, Equals, "3")
}
