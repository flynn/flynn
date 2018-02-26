// This file is an attempt to work with []byte and []rune at the same time while
// preserving information about character width in a terminal.
//
// TODO: improve on this poor design.

package uniline

import "github.com/shinichy/go-wcwidth"

// char represents a character in the terminal screen
// Its size is defined as follows:
// - 1 rune
// - len(char.b) bytes
// - char.colLen terminal columns
type char struct {
	p      []byte
	r      rune
	colLen int
}

func charFromRune(r rune) char {
	return char{[]byte(string(r)), r, wcwidth.WcwidthUcs(r)}
}

func (c char) Clone() char {
	b := make([]byte, len(c.p))
	copy(b, c.p)
	c.p = b
	return c
}

// text represents a sequence of characters
// Its size is defined as follows:
// - len(text.chars) runes
// - len(text.bytes) bytes
// - text.colLen terminal columns
type text struct {
	chars  []char
	bytes  []byte
	colLen int
}

func textFromString(s string) text {
	t := text{chars: make([]char, 0, len(s)), bytes: make([]byte, 0, len(s))}
	for _, r := range s {
		c := charFromRune(r)
		t.chars = append(t.chars, c)
		t.bytes = append(t.bytes, c.p...)
		t.colLen += c.colLen
	}
	return t
}

func (t text) AppendChar(c char) text {
	return text{append(t.chars, c), append(t.bytes, c.p...), t.colLen + c.colLen}
}

func (t text) AppendText(n text) text {
	return text{append(t.chars, n.chars...), append(t.bytes, n.bytes...), t.colLen + n.colLen}
}

func (t text) InsertCharAt(pos position, c char) text {
	chars := make([]char, len(t.chars)+1)
	copy(chars, t.chars[:pos.runes])
	chars[pos.runes] = c
	copy(chars[pos.runes+1:], t.chars[pos.runes:])

	bytes := make([]byte, len(t.bytes)+len(c.p))
	copy(bytes, t.bytes[:pos.bytes])
	copy(bytes[pos.bytes:], c.p)
	copy(bytes[pos.bytes+len(c.p):], t.bytes[pos.bytes:])
	return text{chars, bytes, t.colLen + c.colLen}
}

func (t text) InsertTextAt(pos position, n text) text {
	chars := make([]char, len(t.chars)+len(n.chars))
	copy(chars, t.chars[:pos.runes])
	copy(chars[pos.runes:], n.chars)
	copy(chars[pos.runes+len(n.chars):], t.chars[pos.runes:])

	bytes := make([]byte, len(t.bytes)+len(n.bytes))
	copy(bytes, t.bytes[:pos.bytes])
	copy(bytes[pos.bytes+len(n.bytes):], t.bytes[pos.bytes:])
	copy(bytes[pos.bytes:], n.bytes)

	return text{chars, bytes, t.colLen + n.colLen}
}

func (t text) RemoveCharAt(pos position) text {
	c := t.chars[pos.runes]
	t.bytes = append(t.bytes[:pos.bytes], t.bytes[pos.bytes+len(c.p):]...)
	t.chars = append(t.chars[:pos.runes], t.chars[pos.runes+1:]...)
	t.colLen -= c.colLen
	return t
}

func (t text) Slice(segment ...position) text {
	switch len(segment) {
	case 1:
		t.chars = t.chars[segment[0].runes:]
		t.bytes = t.bytes[segment[0].bytes:]
		t.colLen -= segment[0].columns
	case 2:
		t.chars = t.chars[segment[0].runes:segment[1].runes]
		t.bytes = t.bytes[segment[0].bytes:segment[1].bytes]
		t.colLen = segment[1].columns - segment[0].columns
	default:
		panic("Slice expects 1 or 2 position arguments")
	}
	return t
}

func (t text) Clone() text {
	chars := make([]char, len(t.chars))
	for i, c := range t.chars {
		chars[i] = c.Clone()
	}
	t.chars = chars
	b := make([]byte, len(t.bytes))
	copy(b, t.bytes)
	t.bytes = b
	return t
}

func (t text) String() string {
	return string(t.bytes)
}

type position struct {
	bytes   int
	runes   int
	columns int
}

func (pos position) Add(chars ...char) position {
	for _, c := range chars {
		pos.runes++
		pos.bytes += len(c.p)
		pos.columns += c.colLen
	}
	return pos
}

func (pos position) Subtract(chars ...char) position {
	for _, c := range chars {
		pos.runes--
		pos.bytes -= len(c.p)
		pos.columns -= c.colLen
	}
	return pos
}
