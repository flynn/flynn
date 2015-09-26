package rfc5424

import (
	"bytes"
	"strconv"
	"time"
)

// Parse parses RFC5424 syslog messages into a Message.
func Parse(buf []byte) (*Message, error) {
	msg := &Message{}
	return msg, parse(buf, msg)
}

func parse(buf []byte, msg *Message) error {
	cursor := 0
	if err := parseHeader(buf, &cursor, msg); err != nil {
		return err
	}

	if err := parseStructuredData(buf, &cursor, msg); err != nil {
		return err
	}

	if cursor < len(buf) {
		msg.Msg = buf[cursor:]
	}
	return nil
}

// ParseError will be returned when Parse encounters an error.
type ParseError struct {
	Cursor  int
	Message string
}

func (p *ParseError) Error() string {
	return "rfc5424: " + p.Message
}

// HEADER = PRI VERSION SP TIMESTAMP SP HOSTNAME SP APP-NAME SP PROCID SP MSGID
func parseHeader(buf []byte, cursor *int, msg *Message) error {
	if err := parsePriority(buf, cursor, msg); err != nil {
		return err
	}

	if err := parseVersion(buf, cursor, msg); err != nil {
		return err
	}

	if err := parseTimestamp(buf, cursor, msg); err != nil {
		return err
	}

	var err error
	if msg.Hostname, err = parseNextField(buf, cursor); err != nil {
		return err
	}

	if msg.AppName, err = parseNextField(buf, cursor); err != nil {
		return err
	}

	if msg.ProcID, err = parseNextField(buf, cursor); err != nil {
		return err
	}

	if msg.MsgID, err = parseNextField(buf, cursor); err != nil {
		return err
	}

	return nil
}

func parsePriority(buf []byte, cursor *int, msg *Message) error {
	if len(buf) < *cursor+3 {
		return &ParseError{*cursor, "invalid priority"}
	}
	if buf[*cursor] != priStart {
		return &ParseError{*cursor, "invalid priority"}
	}
	*cursor++
	i := indexByteAfter(buf, priEnd, *cursor)
	if i < 1 || i > 4 {
		return &ParseError{*cursor, "invalid priority: PRIVAL too long"}
	}
	prival, err := strconv.Atoi(string(buf[*cursor:i]))
	if err != nil {
		return err
	}
	if prival < 0 || prival > 191 {
		return &ParseError{*cursor, "invalid priority: PRIVAL outside range"}
	}
	msg.Facility = prival / 8
	msg.Severity = prival % 8
	*cursor = i + 1

	return nil
}

func parseVersion(buf []byte, cursor *int, msg *Message) error {
	if len(buf) < *cursor+2 {
		return &ParseError{*cursor, "message ended before version was received"}
	}
	if buf[*cursor] != '1' || buf[*cursor+1] != ' ' {
		return &ParseError{*cursor, "unexpected syslog version"}
	}
	msg.Version = 1
	*cursor += 2
	return nil
}

func parseTimestamp(buf []byte, cursor *int, msg *Message) error {
	nextSpace := indexByteAfter(buf, ' ', *cursor)
	if nextSpace < *cursor {
		return &ParseError{*cursor, "missing space"}
	}
	if nextSpace == *cursor {
		return &ParseError{*cursor, "missing timestamp"}
	}
	var err error
	msg.Timestamp, err = time.Parse(time.RFC3339Nano, string(buf[*cursor:nextSpace]))
	if err != nil {
		return err
	}
	*cursor = nextSpace + 1
	return nil
}

func parseNextField(buf []byte, cursor *int) ([]byte, error) {
	if len(buf) < *cursor {
		return nil, &ParseError{*cursor, "missing field"}
	}
	nextSpace := indexByteAfter(buf, ' ', *cursor)
	if nextSpace < 0 {
		return nil, &ParseError{*cursor, "missing space"}
	}
	if nextSpace == 1 {
		return nil, &ParseError{*cursor, "missing value"}
	}
	res := buf[*cursor:nextSpace]
	*cursor = nextSpace + 1

	if bytes.Equal(res, nilValue) {
		return nil, nil
	}
	return res, nil
}

func parseStructuredData(buf []byte, cursor *int, msg *Message) error {
	if len(buf) < *cursor {
		return &ParseError{*cursor, "missing structured data field"}
	}
	if c := buf[*cursor]; c == '-' {
		if len(buf) > *cursor+1 && buf[*cursor+1] != ' ' {
			return &ParseError{*cursor, "invalid structured data"}
		}
		if len(buf) > *cursor+2 {
			*cursor++
		}
		*cursor++
		return nil
	} else if c != '[' {
		return &ParseError{*cursor, "invalid structured data"}
	}

	// find the end of the field
	end := *cursor
	for {
		end = indexByteAfter(buf, ']', end+1)
		if end == -1 {
			return &ParseError{*cursor, "invalid structured data"}
		}
		if buf[end-1] != '\\' {
			break
		}
	}
	msg.StructuredData = buf[*cursor : end+1]
	*cursor = end + 2
	return nil
}

func indexByteAfter(buf []byte, c byte, after int) int {
	return bytes.IndexByte(buf[after:], c) + after
}
