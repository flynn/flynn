package uniline

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"unicode"

	"github.com/tiborvass/uniline/ansi"
	"golang.org/x/crypto/ssh/terminal"
)

func defaultOnInterrupt(s *Scanner) (more bool) {
	s.output.Write([]byte("^C"))
	if len(s.buf.bytes) == 0 {
		os.Exit(1)
	}
	s.buf = text{}
	return true
}

// Scanner provides a simple interface to read and, if possible, interactively edit a line using Ansi commands.
type Scanner struct {
	*Core
	onInterrupt func(*Scanner) (more bool)
	km          Keymap
}

type blackhole struct{}

var devNull = new(blackhole)

func (*blackhole) Write(p []byte) (n int, err error) {
	return len(p), nil
}

// DefaultScanner returns a ready-to-use default Scanner.
//
// The input is set to os.Stdin (which is also the output if it's a TTY).
// On Ctrl-C, if the current line is empty, the program exits with status code 1, otherwise returns an empty line.
// The default keymap is the one available in package uniline/
//
// Note: equivalent to NewScanner(nil, nil, nil, nil)
func DefaultScanner() *Scanner {
	return NewScanner(nil, os.Stdout, nil, nil)
}

// NewScanner returns a ready-to-use Scanner with configurable settings.
//
// NewScanner also detects if ANSI-mode is available to let the user edit the input line. If it is not available, it falls back to a dumb-mode
// where scanning is using directly a bufio.Scanner using bufio.ScanLines.
//
// Any parameter can be nil in which case the defaults are used (c.f. DefaultScanner).
//
// In order to have a good line editing experience, input should be an *os.File with the same file descriptor as output
func NewScanner(input io.Reader, output io.Writer, onInterrupt func(s *Scanner) (more bool), km Keymap) *Scanner {
	if input == nil {
		input = os.Stdin
	}
	if onInterrupt == nil {
		onInterrupt = defaultOnInterrupt
	}
	if km == nil {
		km = DefaultKeymap()
	}

	s := &Scanner{&Core{input: input, output: devNull, dumb: true}, onInterrupt, km}

	f, ok := input.(*os.File)
	if !ok {
		return s
	}

	if output != nil {
		_, ok := output.(*os.File)
		if !ok {
			return s
		}
	}
	s.output = input.(io.Writer) // does not panic, since *os.File implements io.Writer

	fd := f.Fd()
	s.fd = &fd
	t := os.Getenv("TERM")
	s.dumb = !terminal.IsTerminal(int(fd)) || len(t) == 0 || t == "dumb" || t == "cons25"
	return s
}

// Scan reads a line from the provided input and makes it available via Scanner.Bytes() and Scanner.Text().
// It returns a boolean indicating whether there can be more lines retrieved or if scanning has ended.
//
// Scanning can end either normally or with an error. The error will be available in Scanner.Err().
//
// If the input source (Scanner.input) is a TTY, the line is editable, otherwise each line is returned.
// Upon Ctrl-C, the current input stops being scanned and Scanner.onInterrupt() whose boolean return value determines whether or not
// scanning should be completely aborted (more = false) or if only the current line should be discarded (more = true), accepting more scans.
func (s *Scanner) Scan(prompt string) (more bool) {
	defer func() {
		// dumb terminals have already printed newline
		if !s.dumb {
			fmt.Fprintln(s.output)
		}
	}()

	defer func() {
		if x := recover(); x != nil {
			var ok bool
			s.err, ok = x.(error)
			if ok {
				// abort scanning because of an encountered error
				more = false
				return
			}
			if sig, ok := x.(os.Signal); ok && sig == os.Interrupt {
				// TODO: reconcile Signals and errors somehow, I don't like having "no error" on a SIGINT.
				s.err = nil
				if s.onInterrupt == nil {
					s.onInterrupt = defaultOnInterrupt
				}
				more = s.onInterrupt(s)
				return
			}
			panic(x)
		}
	}()

	// no need to initialize internal scanner more than once
	if s.scanner == nil {
		s.scanner = bufio.NewScanner(s.input)
	}

	s.prompt = textFromString(prompt)
	s.stop = false

	if s.dumb {
		s.scanner.Split(bufio.ScanLines)

		if _, err := fmt.Fprint(s.output, string(s.prompt.bytes)); err != nil {
			panic(err)
		}

		if !s.scanner.Scan() {
			return false
		}
		// note: buf is of type text, but only "bytes" is used when no tty.
		s.buf.bytes = s.scanner.Bytes()

		s.err = s.scanner.Err()
		// continue scanning if no error
		return s.err == nil
	}
	state, err := terminal.MakeRaw(int(*s.fd))
	if err != nil {
		panic(err)
	}
	defer func() {
		terminal.Restore(int(*s.fd), state)
	}()
	winWidth, _, err := terminal.GetSize(int(*s.fd))
	if err != nil {
		panic(err)
	}

	s.buf = text{}
	s.pos = position{}
	s.cols = int(winWidth)

	// create new empty temporary element in History
	s.history.tmp = append(s.history.tmp, "")
	// set History Index to this newly created empty element
	s.history.index = len(s.history.tmp) - 1

	s.output.Write(s.prompt.bytes)
	s.scanner.Split(bufio.ScanRunes)

	var p []byte

	for !s.stop && s.scanner.Scan() {

		var r rune

		var isCompleteAnsiCode = func() (done bool) {
			key := ansi.Code(p)
			scanFun, ok := s.km[key]
			if ok {
				if scanFun == nil {
					return false
				}
				scanFun(s.Core)
			}
			return true
		}

		if p == nil {
			// In case where p is either one-rune long or it is the first byte of a long command

			p = s.scanner.Bytes()
			r = getRune(string(p))

			// if printable, then it's not a command
			if unicode.IsPrint(r) {
				s.Insert(charFromRune(r))
				// moving on to next rune
				p = nil
				continue
			}

			//Debug("r: %v", r)

			if isCompleteAnsiCode() {
				// moving on to next rune
				p = nil
			}

			// handle special case for Clipboard
			if r != 23 && r != 21 && r != 11 {
				// not Ctrl-W, Ctrl-U, or Ctrl-K
				// thus consider the Clipboard as complete and stop gluing Clipboard parts together
				s.clipboard.partial = false
			}

		} else {
			// In the case where p is an escape sequence, add current bytes to previous and try a lookup

			p = append(p, s.scanner.Bytes()...)
			//Debug("p: %v", p)
			if isCompleteAnsiCode() {
				p = nil
				//Debug("done")
			}
		}
	}

	s.err = s.scanner.Err()
	// if EOF, we need to consider last line
	if !s.stop {
		s.Enter()
		return false
	}
	return s.err == nil
}

// Err returns the first non-EOF error that was encountered by the Scanner.
func (s *Scanner) Err() error {
	return s.err
}

// text returns the most recent line read from s.input during a call to Scan as a newly allocated string holding its bytes.
func (s *Scanner) Text() string {
	return string(s.buf.bytes)
}

// Bytes returns the most recent line read from s.input during a call to Scan.
// The underlying array may point to data that will be overwritten by subsequent call to Scan.
// It does no allocation.
func (s *Scanner) Bytes() []byte {
	return s.buf.bytes
}

// trick to get the first rune of a string without utf8 package
func getRune(str string) rune {
	var i int
	var r rune
	for i, r = range str {
		if i > 0 {
			panic("ScanRunes is supposed to scan one rune at a time, but received more than one")
		}
	}
	return r
}
