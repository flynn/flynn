package snapshot

import (
	"encoding/gob"
	"io"

	"github.com/flynn/flynn/pkg/syslog/rfc5424"
)

// WriteTo writes a snapshot of the buffers to the writer. The partitioning of
// messages is not retained. The writer is left open.
func WriteTo(buffers [][]*rfc5424.Message, w io.Writer) error {
	enc := gob.NewEncoder(w)
	return writeTo(buffers, enc)
}

func writeTo(buffers [][]*rfc5424.Message, enc *gob.Encoder) error {
	for _, buf := range buffers {
		for _, msg := range buf {
			if err := enc.Encode(msg); err != nil {
				return err
			}
		}
	}
	return nil
}

// StreamTo writes a snapshot of the buffers to the writer, then writes
// messages from the channel to the writer. The writer is left open.
func StreamTo(buffers [][]*rfc5424.Message, msgc <-chan *rfc5424.Message, w io.Writer) error {
	enc := gob.NewEncoder(w)
	if err := writeTo(buffers, enc); err != nil {
		return err
	}

	for msg := range msgc {
		if err := enc.Encode(msg); err != nil {
			return err
		}
	}
	return nil
}

type Scanner struct {
	dec *gob.Decoder
	err error

	// Current Message, updated on each call to Scan.
	Message *rfc5424.Message
}

// NewScanner returns a new Scanner reading from r.
func NewScanner(r io.Reader) *Scanner {
	return &Scanner{dec: gob.NewDecoder(r)}
}

func (s *Scanner) Scan() bool {
	msg := &rfc5424.Message{}
	err := s.dec.Decode(msg)
	if err != nil {
		s.err = err
		return false
	}
	s.Message = msg
	return true
}

func (s *Scanner) Err() error {
	if s.err != io.EOF {
		return s.err
	}
	return nil
}
