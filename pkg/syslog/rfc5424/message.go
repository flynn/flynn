package rfc5424

import (
	"bytes"
	"fmt"
	"time"
)

const (
	priStart = '<'
	priEnd   = '>'
)

var nilValue = []byte{'-'}

type Message struct {
	Header
	StructuredData []byte
	Msg            []byte
}

// NewMessage builds a new message from a copy of the header and message.
func NewMessage(hdr *Header, msg []byte) *Message {
	var h Header
	if hdr != nil {
		h = *hdr
	}

	if h.Timestamp.IsZero() {
		h.Timestamp = time.Now().UTC()
	}

	if h.Version == 0 {
		h.Version = 1
	}

	if msg != nil {
		m := make([]byte, len(msg))
		copy(m, msg)
		msg = m
	}

	return &Message{Header: h, Msg: msg}
}

var msgSep = []byte{' '}

func (m Message) Bytes() []byte {
	sd := m.StructuredData
	if len(sd) == 0 {
		sd = nilValue
	}
	if len(m.Msg) > 0 {
		return bytes.Join([][]byte{m.Header.Bytes(), sd, m.Msg}, msgSep)
	}
	return bytes.Join([][]byte{m.Header.Bytes(), sd}, msgSep)
}

func (m Message) String() string {
	return string(m.Bytes())
}

type Header struct {
	Facility  int
	Severity  int
	Version   int
	Timestamp time.Time
	Hostname  []byte
	AppName   []byte
	ProcID    []byte
	MsgID     []byte
}

const syslogTimestamp = "2006-01-02T15:04:05.000000Z07:00"

func (h Header) Bytes() []byte {
	hostname := h.Hostname
	if len(hostname) == 0 {
		hostname = nilValue
	}

	appName := h.AppName
	if len(appName) == 0 {
		appName = nilValue
	}

	procID := h.ProcID
	if len(procID) == 0 {
		procID = nilValue
	}

	msgID := h.MsgID
	if len(msgID) == 0 {
		msgID = nilValue
	}

	buf := &bytes.Buffer{}
	fmt.Fprintf(buf, "<%d>%d %s %s %s %s %s",
		h.PriVal(),
		1,
		h.Timestamp.Format(syslogTimestamp),
		hostname,
		appName,
		procID,
		msgID)
	return buf.Bytes()
}

func (h Header) PriVal() int {
	return h.Facility*8 + h.Severity
}
