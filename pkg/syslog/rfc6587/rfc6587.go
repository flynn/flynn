package rfc6587

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"

	"github.com/flynn/flynn/pkg/syslog/rfc5424"
)

// Bytes returns the RFC6587-framed bytes of an RFC5424 syslog Message,
// including length prefix.
func Bytes(m *rfc5424.Message) []byte {
	msg := m.Bytes()
	return bytes.Join([][]byte{[]byte(strconv.Itoa(len(msg))), msg}, []byte{' '})
}

// MaxMsgLen is the maximum allowed length of the entire syslog message
// (excluding RFC6587 length-prefixed frame).
const MaxMsgLen = 10100

// Split is a bufio.SplitFunc that splits on RFC6587-framed syslog messages.
func Split(data []byte, atEOF bool) (advance int, token []byte, err error) {
	return split(false, data, atEOF)
}

// SplitWithNewlines is a bufio.SplitFunc that splits on RFC6587-framed syslog
// messages that are each followed by a newline.
func SplitWithNewlines(data []byte, atEOF bool) (advance int, token []byte, err error) {
	return split(true, data, atEOF)
}

func split(newlines bool, data []byte, atEOF bool) (advance int, token []byte, err error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}

	i := bytes.IndexByte(data, ' ')
	switch {
	case i == 0:
		return 0, nil, errors.New("expected MSG-LEN, got space")
	case i > 5:
		return 0, nil, errors.New("MSG-LEN was longer than 5 characters")
	case i > 0:
		msgLen := data[0:i]
		length, err := strconv.Atoi(string(msgLen))
		if err != nil {
			return 0, nil, err
		}
		if length > MaxMsgLen {
			return 0, nil, fmt.Errorf("maximum MSG-LEN is %d, got %d", MaxMsgLen, length)
		}
		if newlines {
			length++
		}
		end := length + i + 1
		if len(data) >= end {
			// Return frame without msg length
			return end, data[i+1 : end], nil
		}
	}
	// Request more data.
	return 0, nil, nil
}
