package grohl

import (
	"fmt"
	"io"
	"os"
)

// IoLogger assembles the key/value pairs into a line and writes it to any
// io.Writer.  This expects the writers to be threadsafe.
type IoLogger struct {
	stream  io.Writer
	AddTime bool
}

func NewIoLogger(stream io.Writer) *IoLogger {
	if stream == nil {
		stream = os.Stdout
	}

	return &IoLogger{stream, true}
}

// Log writes the assembled log line.
func (l *IoLogger) Log(data Data) error {
	line := fmt.Sprintf("%s\n", BuildLog(data, l.AddTime))
	_, err := l.stream.Write([]byte(line))
	return err
}

// ChannelLogger sends the key/value data to a channel.  This is useful when
// loggers are in separate goroutines.
type ChannelLogger struct {
	channel chan Data
}

func NewChannelLogger(channel chan Data) (*ChannelLogger, chan Data) {
	if channel == nil {
		channel = make(chan Data)
	}
	return &ChannelLogger{channel}, channel
}

// Log writes the assembled log line.
func (l *ChannelLogger) Log(data Data) error {
	l.channel <- data
	return nil
}

// Watch starts a for loop that sends any output from logch to logger.Log().
// This is intended to be used in a goroutine.
func Watch(logger Logger, logch chan Data) {
	for {
		data := <-logch
		if data != nil {
			logger.Log(data)
		} else {
			return
		}
	}
}
