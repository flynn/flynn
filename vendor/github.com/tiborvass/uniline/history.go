package uniline

import (
	"bufio"
	"fmt"
	"os"
)

// ClearHistory Clears history.
func (s *Scanner) ClearHistory() {
	s.history = history{}
}

// AddToHistory adds a string line to history
func (s *Scanner) AddToHistory(line string) {
	s.history.tmp = append(s.history.tmp, line)
	s.history.saved = append(s.history.saved, line)
}

// SaveHistory saves the current history to a file specified by filename.
func (s *Scanner) SaveHistory(filename string) error {
	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer f.Close()
	for _, line := range s.history.tmp[:len(s.history.tmp)-1] {
		if _, err := fmt.Fprintln(f, line); err != nil {
			return err
		}
	}
	return nil
}

// LoadHistory loads history from a file specified by filename.
func (s *Scanner) LoadHistory(filename string) error {
	f, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	lines := []string{}
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return err
	}

	// tmp = saved = loaded History
	s.history.saved = lines
	s.history.tmp = make([]string, len(s.history.saved))
	copy(s.history.tmp, s.history.saved)

	// add current line
	s.history.tmp = append(s.history.tmp, s.buf.String())
	s.history.index = len(s.history.tmp) - 1
	return nil
}
