package rfc5424

import (
	"bytes"
	"fmt"
	"io"
)

type StructuredData struct {
	ID     []byte
	Params []StructuredDataParam
}

func (e StructuredData) Encode(w io.Writer) error {
	if len(e.ID) == 0 {
		return writeByte(w, '-')
	}

	writeByte(w, '[')
	w.Write(e.ID)
	writeByte(w, ' ')
	for i, p := range e.Params {
		p.Encode(w)
		if i != len(e.Params)-1 {
			writeByte(w, ' ')
		}
	}
	return writeByte(w, ']')
}

func (s StructuredData) String() string {
	res := fmt.Sprintf("[%s ", s.ID)
	for i, e := range s.Params {
		res += e.String()
		if i != len(s.Params)-1 {
			res += " "
		}
	}
	res += "]"
	return res
}

type StructuredDataParam struct {
	Name  []byte
	Value []byte
}

func (s StructuredDataParam) String() string {
	return fmt.Sprintf("%s=%s", s.Name, s.Value)
}

func (s *StructuredDataParam) Encode(w io.Writer) error {
	w.Write(s.Name)
	writeByte(w, '=')
	writeByte(w, '"')
	for _, b := range s.Value {
		if b == '"' || b == '\\' || b == ']' {
			writeByte(w, '\\')
		}
		writeByte(w, b)
	}
	return writeByte(w, '"')
}

func writeByte(w io.Writer, b byte) error {
	if bw, ok := w.(io.ByteWriter); ok {
		return bw.WriteByte(b)
	}
	_, err := w.Write([]byte{b})
	return err
}

func ParseStructuredData(buf []byte) (*StructuredData, error) {
	if len(buf) == 1 && buf[0] == '-' {
		return nil, nil
	}
	if len(buf) < 2 || buf[0] != '[' || buf[len(buf)-1] != ']' {
		return nil, &ParseError{0, "invalid structured data"}
	}

	res := &StructuredData{
		Params: make([]StructuredDataParam, 0, bytes.Count(buf, []byte{'='})),
	}

	cursor := 1
	parseName := func(delimiters ...byte) ([]byte, error) {
		var res []byte
	outer:
		for i, b := range buf[cursor:] {
			for _, d := range delimiters {
				if b == d {
					res = buf[cursor : cursor+i]
					cursor += i + 1
					break outer
				}
			}
			if b == ' ' || b == '=' || b == ']' || b == '"' {
				return nil, &ParseError{cursor + i, fmt.Sprintf("unexpected %q parsing structured data name", b)}
			}
		}
		if len(res) == 0 {
			return nil, &ParseError{cursor, "invalid structured data name"}
		}
		return res, nil
	}

	var err error
	res.ID, err = parseName(' ', ']')
	if err != nil {
		return nil, err
	}

	if len(buf) == cursor {
		return res, nil
	}

outer:
	for {
		var d StructuredDataParam
		d.Name, err = parseName('=')
		if err != nil {
			return nil, err
		}
		if len(buf) < cursor+3 {
			return nil, &ParseError{cursor, "invalid structured data, expected value"}
		}
		if buf[cursor] != '"' {
			return nil, &ParseError{cursor, "invalid structured data value, expected '\"'"}
		}
		cursor++

		lastEscape := false
		closed := false
		var valBuf bytes.Buffer
	value:
		for _, b := range buf[cursor:] {
			cursor++
			switch b {
			case '\\':
				if !lastEscape {
					lastEscape = true
					continue
				}
			case ']':
				if !lastEscape {
					return nil, &ParseError{cursor, "invalid structured data, unexpected ']'"}
				}
			case '"':
				if !lastEscape {
					closed = true
					break value
				}
			default:
				if lastEscape {
					valBuf.WriteByte('\\')
				}
			}
			valBuf.WriteByte(b)
			lastEscape = false
		}
		if !closed {
			return nil, &ParseError{cursor, "unexpected end of structured data"}
		}
		d.Value = valBuf.Bytes()
		res.Params = append(res.Params, d)

		if len(buf) < cursor+1 {
			return nil, &ParseError{cursor, "unexpected end of structured data, no closing ']'"}
		}
		switch buf[cursor] {
		case ']':
			if len(buf) > cursor+1 {
				return nil, &ParseError{cursor, "unexpected end of structured data, "}
			}
			break outer
		case ' ':
			if len(buf) < cursor+5 || buf[cursor+1] == ']' {
				return nil, &ParseError{cursor, "unexpected space"}
			}
		default:
			return nil, &ParseError{cursor, fmt.Sprintf("unexpected structured data character %q", buf[cursor])}
		}
		cursor++
	}

	return res, nil
}
