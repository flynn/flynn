package rfc5424

import (
	"bytes"
	"errors"
	"strconv"
	"time"
)

type Message struct {
	Facility       int
	Severity       int
	Version        int
	Timestamp      time.Time
	Hostname       string
	AppName        string
	ProcID         string
	MsgID          string
	StructuredData string
	Msg            string
}

func (m Message) PriVal() int {
	return m.Facility*8 + m.Severity
}

func Parse(buf []byte) (*Message, error) {
	cursor := 0
	msg := &Message{}
	if err := parseHeader(buf, &cursor, msg); err != nil {
		return nil, err
	}

	if err := parseStructuredData(buf, &cursor, msg); err != nil {
		return nil, err
	}

	if cursor < len(buf) {
		msg.Msg = string(buf[cursor:])
	}

	return msg, nil
}

const (
	priStart = '<'
	priEnd   = '>'
)

// HEADER = PRI VERSION SP TIMESTAMP SP HOSTNAME SP APP-NAME SP PROCID SP MSGID
func parseHeader(buf []byte, cursor *int, msg *Message) error {
	var err error
	if err = parsePriority(buf, cursor, msg); err != nil {
		return err
	}

	if err = parseVersion(buf, cursor, msg); err != nil {
		return err
	}

	if err = parseTimestamp(buf, cursor, msg); err != nil {
		return err
	}

	if msg.Hostname, err = parseNextStringField(buf, cursor); err != nil {
		return err
	}

	if msg.AppName, err = parseNextStringField(buf, cursor); err != nil {
		return err
	}

	if msg.ProcID, err = parseNextStringField(buf, cursor); err != nil {
		return err
	}

	if msg.MsgID, err = parseNextStringField(buf, cursor); err != nil {
		return err
	}

	return nil
}

func parsePriority(buf []byte, cursor *int, msg *Message) error {
	if len(buf) < *cursor+3 {
		return errors.New("invalid priority")
	}
	if buf[*cursor] != priStart {
		return errors.New("invalid priority")
	}
	*cursor++
	i := indexByteAfter(buf, priEnd, *cursor)
	if i < 1 || i > 4 {
		return errors.New("invalid priority")
	}
	prival, err := strconv.Atoi(string(buf[*cursor:i]))
	if err != nil {
		return err
	}
	if prival < 0 || prival > 191 {
		return errors.New("invalid priority")
	}
	msg.Facility = prival / 8
	msg.Severity = prival % 8
	*cursor = i + 1

	return nil
}

func parseVersion(buf []byte, cursor *int, msg *Message) error {
	if len(buf) < *cursor+1 {
		return errors.New("message ended before version was received")
	}
	if buf[*cursor] != '1' || buf[*cursor+1] != ' ' {
		return errors.New("unexpected syslog version")
	}
	msg.Version = 1
	*cursor += 2
	return nil
}

func parseTimestamp(buf []byte, cursor *int, msg *Message) error {
	var err error
	nextSpace := indexByteAfter(buf, ' ', *cursor)
	if nextSpace < *cursor {
		return errors.New("missing space")
	}
	if nextSpace == *cursor {
		return errors.New("missing timestamp")
	}
	msg.Timestamp, err = time.Parse(time.RFC3339Nano, string(buf[*cursor:nextSpace]))
	if err != nil {
		return err
	}
	*cursor = nextSpace + 1
	return nil
}

func parseNextStringField(buf []byte, cursor *int) (string, error) {
	if len(buf) < *cursor {
		return "", errors.New("missing field")
	}
	nextSpace := indexByteAfter(buf, ' ', *cursor)
	if nextSpace < 0 {
		return "", errors.New("missing space")
	}
	if nextSpace == 1 {
		return "", errors.New("missing value")
	}
	res := string(buf[*cursor:nextSpace])
	*cursor = nextSpace + 1
	return res, nil
}

func parseStructuredData(buf []byte, cursor *int, msg *Message) error {
	if len(buf) < *cursor {
		return errors.New("missing structured data field")
	}
	if buf[*cursor] == '-' {
		if len(buf) < *cursor+1 || buf[*cursor+1] != ' ' {
			return errors.New("invalid structured data")
		}
		*cursor++
		msg.StructuredData = "-"
		return nil
	}
	return errors.New("structured data is unsupported")
}

func indexByteAfter(buf []byte, c byte, after int) int {
	return bytes.IndexByte(buf[after:], c) + after
}
