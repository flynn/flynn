package uniline

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"unicode"

	"github.com/tiborvass/uniline/ansi"
)

// Data structure of the internals of Scanner, useful when creating a custom Keymap.
type Core struct {
	input     io.Reader
	output    io.Writer
	scanner   *bufio.Scanner
	prompt    text
	history   history
	clipboard clipboard
	pos       position
	cols      int // number of columns, aka window width
	buf       text
	err       error // the error that will be returned in Err()
	dumb      bool
	fd        *uintptr

	// Whether to stop current line's scanning
	// This is used for internal scanning.
	// Termination of external scanning is handled with the boolean return variable `more`
	stop bool
}

type history struct {
	saved []string
	tmp   []string
	index int
}

type clipboard struct {
	text    text
	partial bool
}

func (core *Core) Insert(c char) {
	if core.buf.colLen == core.pos.columns {
		core.buf = core.buf.AppendChar(c)
		core.pos = core.pos.Add(c)
		if core.prompt.colLen+core.buf.colLen < core.cols { // TODO: handle multiline
			mustWrite(core.output.Write(c.p))
		} else {
			core.Refresh()
		}
	} else {
		core.buf = core.buf.InsertCharAt(core.pos, c)
		core.pos = core.pos.Add(c)
		core.Refresh()
	}
}

func (core *Core) Enter() {
	// removing most recent element of History
	// if user actually wants to add it, he can call Scanner.AddToHistory(line)
	core.history.tmp = core.history.tmp[:len(core.history.tmp)-1]

	// Note: Design decision (differs from the readline in bash)
	//
	// > foo⏎ 			(yields "foo" + assuming it is added to History)
	// > bar⏎ 			(yields "bar" + assuming it is added to History)
	// > ↑				(going 1 element back in History)
	// > bar
	// > bar2 			(modifying bar element; note that Enter was not hit)
	// > ↑				(going 1 element back in History)
	// > foo
	// > foo42			(modifying foo element)
	// > foo42⏎			(hitting Enter, yields "foo42" + assuming it is added to History)
	//
	// At the end, History looks like ["foo", "bar", "foo42"] losing "bar2".
	// This is differing from bash where History would look like ["foo", "bar2", "foo42"] losing "bar".
	copy(core.history.tmp, core.history.saved)
	core.stop = true
}

func (core *Core) Interrupt() {
	core.history.tmp = core.history.tmp[:len(core.history.tmp)-1]
	panic(os.Interrupt)
}

func (core *Core) DeleteOrEOF() {
	if len(core.buf.chars) == 0 {
		var err error
		// since err is of type error and is nil, it will result in a clean EOF
		// look at the defer in Scan() in uniline.go
		panic(err)
	}
	core.Delete()
}

func (core *Core) Backspace() {
	if core.pos.runes > 0 && len(core.buf.chars) > 0 {
		c := core.buf.chars[core.pos.runes-1]
		pos2 := core.pos.Subtract(c)
		core.buf = core.buf.RemoveCharAt(pos2)
		core.pos = pos2
		core.Refresh()
	} else {
		core.Bell()
	}
}

func (core *Core) Delete() {
	if len(core.buf.chars) > 0 && core.pos.runes < len(core.buf.chars) {
		core.buf = core.buf.RemoveCharAt(core.pos)
		core.Refresh()
	} else {
		core.Bell()
	}
}

func (core *Core) MoveLeft() {
	if core.pos.runes > 0 {
		core.pos = core.pos.Subtract(core.buf.chars[core.pos.runes-1])
		core.Refresh()
	} else {
		core.Bell()
	}
}

func (core *Core) MoveRight() {
	if core.pos.runes < len(core.buf.chars) {
		core.pos = core.pos.Add(core.buf.chars[core.pos.runes])
		core.Refresh()
	} else {
		core.Bell()
	}
}

func (core *Core) MoveWordLeft() {
	if core.pos.runes > 0 {
		var nonSpaceEncountered bool
		for pos := core.pos.runes - 1; pos >= 0; pos-- {
			c := core.buf.chars[pos]
			if unicode.IsSpace(c.r) {
				if nonSpaceEncountered {
					break
				}
			} else if !nonSpaceEncountered {
				nonSpaceEncountered = true
			}
			core.pos = core.pos.Subtract(c)
		}
		core.Refresh()
	}
}

func (core *Core) MoveWordRight() {
	if core.pos.runes < len(core.buf.chars) {
		var nonSpaceEncountered bool
		for pos := core.pos.runes; pos < len(core.buf.chars); pos++ {
			c := core.buf.chars[pos]
			if unicode.IsSpace(c.r) {
				if nonSpaceEncountered {
					break
				}
			} else if !nonSpaceEncountered {
				nonSpaceEncountered = true
			}
			core.pos = core.pos.Add(c)
		}
		core.Refresh()
	}
}

func (core *Core) MoveBeginning() {
	core.pos = position{}
	core.Refresh()
}

func (core *Core) MoveEnd() {
	core.pos = position{len(core.buf.chars), len(core.buf.bytes), core.buf.colLen}
	core.Refresh()
}

func (core *Core) HistoryBack() {
	if core.history.index > 0 {
		core.history.tmp[core.history.index] = core.buf.String()
		core.history.index--
		core.buf = textFromString(core.history.tmp[core.history.index])
		core.pos = position{len(core.buf.chars), len(core.buf.bytes), core.buf.colLen}
		core.Refresh()
	} else {
		core.Bell()
	}
}

func (core *Core) HistoryForward() {
	if core.history.index < len(core.history.tmp)-1 {
		core.history.tmp[core.history.index] = core.buf.String()
		core.history.index++
		core.buf = textFromString(core.history.tmp[core.history.index])
		core.pos = position{len(core.buf.chars), len(core.buf.bytes), core.buf.colLen}
		core.Refresh()
	} else {
		core.Bell()
	}
}

func (core *Core) CutLineLeft() {
	if core.pos.runes > 0 {
		if core.clipboard.partial {
			core.clipboard.text = core.buf.Slice(position{}, core.pos).AppendText(core.clipboard.text)
		} else {
			core.clipboard.text = core.buf.Slice(position{}, core.pos)
		}
		core.buf = core.buf.Slice(core.pos)
		core.pos = position{}
		core.Refresh()
	} else {
		core.Bell()
	}
}

func (core *Core) CutLineRight() {
	if core.pos.runes < len(core.buf.chars) {
		if core.clipboard.partial {
			core.clipboard.text = core.clipboard.text.AppendText(core.buf.Slice(core.pos).Clone())
		} else {
			core.clipboard.text = core.buf.Slice(core.pos).Clone()
		}
		core.clipboard.partial = true
		core.buf = core.buf.Slice(position{}, core.pos)
		core.Refresh()
	}
}

func (core *Core) CutPrevWord() {
	if core.pos.runes > 0 {
		pos := core.pos
		var nonSpaceEncountered bool
		for pos.runes > 0 {
			if unicode.IsSpace(core.buf.chars[pos.runes-1].r) {
				if nonSpaceEncountered {
					break
				}
			} else if !nonSpaceEncountered {
				nonSpaceEncountered = true
			}
			pos = pos.Subtract(core.buf.chars[pos.runes-1])
		}
		if core.clipboard.partial {
			core.clipboard.text = core.buf.Slice(pos, core.pos).Clone().AppendText(core.clipboard.text)
		} else {
			core.clipboard.text = core.buf.Slice(pos, core.pos).Clone()
		}
		core.clipboard.partial = true
		core.buf = core.buf.Slice(position{}, pos).AppendText(core.buf.Slice(core.pos))
		core.pos = pos
		core.Refresh()
	} else {
		core.Bell()
	}
}

func (core *Core) SwapChars() {
	if core.pos.runes > 0 && len(core.buf.chars) > 1 {
		pos := core.pos
		if core.pos.runes == len(core.buf.chars) {
			pos = pos.Subtract(core.buf.chars[core.pos.runes-1])
		}
		core.buf.chars[pos.runes-1], core.buf.chars[pos.runes] = core.buf.chars[pos.runes], core.buf.chars[pos.runes-1]
		core.buf.bytes[pos.bytes-1], core.buf.bytes[pos.bytes] = core.buf.bytes[pos.bytes], core.buf.bytes[pos.bytes-1]
		core.pos = pos.Add(core.buf.chars[pos.runes])
		core.Refresh()
	} else {
		core.Bell()
	}
}

func (core *Core) Paste() {
	core.buf = core.buf.InsertTextAt(core.pos, core.clipboard.text)
	core.pos = core.pos.Add(core.clipboard.text.chars...)
	core.Refresh()
}

func (core *Core) Clear() {
	mustWrite(core.output.Write([]byte(ansi.ClearScreen)))
	core.Refresh()
}

func (core *Core) Bell() {
	mustWrite(core.output.Write([]byte(ansi.Bell)))
}

func (core *Core) Refresh() {
	buf := core.buf
	pos := core.pos
	pos2 := position{}
	x := buf.colLen

	for core.prompt.colLen+pos.columns >= core.cols {
		c := buf.chars[pos2.runes]
		pos2 = pos2.Add(c)
		pos = pos.Subtract(c)
		x -= c.colLen
	}
	pos3 := pos2
	for pos3.columns < buf.colLen {
		pos3 = pos3.Add(buf.chars[pos3.runes])
	}
	for core.prompt.colLen+x >= core.cols {
		c := buf.chars[pos3.runes-1]
		pos3 = pos3.Subtract(c)
		x -= c.colLen
	}
	buf = buf.Slice(pos2, pos3)

	mustWrite(fmt.Fprintf(core.output, "%s%s%s%s%s",
		ansi.CursorToLeftEdge,
		core.prompt.bytes,
		buf.bytes,
		ansi.EraseToRight,
		fmt.Sprintf(ansi.MoveCursorForward, core.prompt.colLen+pos.columns),
	))
}

func mustWrite(n int, err error) int {
	if err != nil {
		panic(err)
	}
	return n
}
