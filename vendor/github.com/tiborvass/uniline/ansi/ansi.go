package ansi

type Code string

// Input keys
const (
	NEWLINE         Code = "\n"
	CARRIAGE_RETURN      = "\r"
	CTRL_C               = "\x03"
	CTRL_D               = "\x04"
	CTRL_H               = "\x08"
	BACKSPACE            = "\x7f"
	CTRL_L               = "\x0c"
	CTRL_T               = "\x14"
	CTRL_B               = "\x02"
	CTRL_F               = "\x06"
	CTRL_P               = "\x10"
	CTRL_N               = "\x0e"
	CTRL_U               = "\x15"
	CTRL_K               = "\x0b"
	CTRL_A               = "\x01"
	CTRL_E               = "\x05"
	CTRL_W               = "\x17"
	CTRL_Y               = "\x19"

	META_B     = "\x1bb"
	META_LEFT  = "\x1bB"
	META_F     = "\x1bf"
	META_RIGHT = "\x1bF"

	LEFT  = "\x1b[D"
	RIGHT = "\x1b[C"
	UP    = "\x1b[A"
	DOWN  = "\x1b[B"

	DELETE = "\x1b[3\x7e"
)

// Partial codes (beginning of a potentially valid ANSI code)
const (
	START_ESCAPE_SEQ            Code = "\x1b"
	START_EXTENDED_ESCAPE_SEQ        = "\x1b["
	START_EXTENDED_ESCAPE_SEQ_0      = "\x1b[0"
	START_EXTENDED_ESCAPE_SEQ_1      = "\x1b[1"
	START_EXTENDED_ESCAPE_SEQ_2      = "\x1b[2"
	START_EXTENDED_ESCAPE_SEQ_3      = "\x1b[3"
	START_EXTENDED_ESCAPE_SEQ_4      = "\x1b[4"
	START_EXTENDED_ESCAPE_SEQ_5      = "\x1b[5"
	START_EXTENDED_ESCAPE_SEQ_6      = "\x1b[6"
	START_EXTENDED_ESCAPE_SEQ_7      = "\x1b[7"
	START_EXTENDED_ESCAPE_SEQ_8      = "\x1b[8"
	START_EXTENDED_ESCAPE_SEQ_9      = "\x1b[9"
)

// Output commands
const (
	CursorToLeftEdge  Code = "\x1b[0G"
	Bell                   = "\x07"
	EraseToRight           = "\x1b[K"
	ClearScreen            = "\x1b[H\x1b[2J"
	MoveCursorForward      = "\x1b[0G\x1b[%dC" // format string expecting an integer (%d)
)
