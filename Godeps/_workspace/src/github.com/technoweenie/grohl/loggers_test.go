package grohl

import (
	"bytes"
	"strings"
	"testing"
)

func TestIoLog(t *testing.T) {
	buf := bytes.NewBufferString("")
	logger := NewIoLogger(buf)
	logger.AddTime = false
	logger.Log(Data{"a": 1})
	expected := "a=1\n"

	if actual := buf.String(); actual != expected {
		t.Errorf("e: %s\na: %s", expected, actual)
	}
}

func TestChannelLog(t *testing.T) {
	channel := make(chan Data, 1)
	logger, channel := NewChannelLogger(channel)
	data := Data{"a": 1}
	logger.Log(data)

	recv := <-channel

	if recvKeys := len(recv); recvKeys != len(data) {
		t.Errorf("Wrong number of keys: %d (%s)", recvKeys, recv)
	}

	if data["a"] != recv["a"] {
		t.Errorf("Received: %s", recv)
	}
}

type loggerBuffer struct {
	channel chan Data
	t       *testing.T
}

func (b *loggerBuffer) String() string {
	close(b.channel)
	lines := make([]string, len(b.channel))
	i := 0

	for data := range b.channel {
		lines[i] = BuildLog(data, false)
		i = i + 1
	}

	return strings.Join(lines, "\n")
}

func (b *loggerBuffer) AssertLogged(expected string) {
	if result := b.String(); result != expected {
		b.t.Errorf("Bad log output: %s", result)
	}
}

func setupLogger(t *testing.T) (*Context, *loggerBuffer) {
	ch := make(chan Data, 100)
	logger, _ := NewChannelLogger(ch)
	context := NewContext(nil)
	context.Logger = logger
	return context, &loggerBuffer{ch, t}
}
