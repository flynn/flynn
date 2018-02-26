package uniline

import (
	"github.com/tiborvass/uniline/ansi"
)

// Keymap is a hash table mapping Ansi codes to functions.
// There are no locks on this map, if accessing it concurrently
// please consider it as a static map (1 initial write, THEN as many concurrent reads as you wish)
type Keymap map[ansi.Code]func(*Core)

// DefaultKeymap returns a copy of the default Keymap
// Useful if inspection/customization is needed.
func DefaultKeymap() Keymap {
	return Keymap{
		ansi.NEWLINE:         (*Core).Enter,
		ansi.CARRIAGE_RETURN: (*Core).Enter,
		ansi.CTRL_C:          (*Core).Interrupt,
		ansi.CTRL_D:          (*Core).DeleteOrEOF,
		ansi.CTRL_H:          (*Core).Backspace,
		ansi.BACKSPACE:       (*Core).Backspace,
		ansi.CTRL_L:          (*Core).Clear,
		ansi.CTRL_T:          (*Core).SwapChars,

		ansi.CTRL_B: (*Core).MoveLeft,
		ansi.CTRL_F: (*Core).MoveRight,
		ansi.CTRL_P: (*Core).HistoryBack,
		ansi.CTRL_N: (*Core).HistoryForward,

		ansi.CTRL_U: (*Core).CutLineLeft,
		ansi.CTRL_K: (*Core).CutLineRight,
		ansi.CTRL_A: (*Core).MoveBeginning,
		ansi.CTRL_E: (*Core).MoveEnd,
		ansi.CTRL_W: (*Core).CutPrevWord,
		ansi.CTRL_Y: (*Core).Paste,

		// Escape sequences
		ansi.START_ESCAPE_SEQ: nil,

		ansi.META_B:     (*Core).MoveWordLeft,
		ansi.META_LEFT:  (*Core).MoveWordLeft,
		ansi.META_F:     (*Core).MoveWordRight,
		ansi.META_RIGHT: (*Core).MoveWordRight,

		ansi.LEFT:  (*Core).MoveLeft,
		ansi.RIGHT: (*Core).MoveRight,
		ansi.UP:    (*Core).HistoryBack,
		ansi.DOWN:  (*Core).HistoryForward,

		// Extended escape
		ansi.START_EXTENDED_ESCAPE_SEQ:   nil,
		ansi.START_EXTENDED_ESCAPE_SEQ_3: nil,

		ansi.DELETE: (*Core).Delete, // Delete key
	}
}
