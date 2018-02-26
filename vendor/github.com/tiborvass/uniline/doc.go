/*
 Author: Tibor Vass (gh: @tiborvass)
 License: MIT
*/

/*
Package uniline provides a simple readline API written in pure Go with Unicode support.
It allows users to interactively input and edit a line from the terminal.

Most of the usual GNU readline capabilities and control keys are implemented.
If the provided input source is not a TTY or not an ANSI-compatible TTY, uniline falls back to scanning each line using bufio.ScanLines.

Features
	Unicode
	Optional History (search coming soon)
	Fallback for non-TTY or Dumb terminals
	Single line editing (multiline coming soon)

Supported Keys
	Left / Ctrl-B
	Right / Ctrl-F
	Up / Ctrl-P
	Down / Ctrl-N

	Meta-Left
	Meta-Right

	Backspace / Ctrl-H
	Delete
	Ctrl-A
	Ctrl-E

	Ctrl-T
	Ctrl-W
	Ctrl-K
	Ctrl-U
	Ctrl-Y
	Ctrl-L

	Ctrl-C
	Ctrl-D

Echo Example (available in examples/echo/echo.go):

	package main

	import (
		"fmt"
		"github.com/tiborvass/uniline"
	)

	func main() {
		prompt := "> "
		scanner := uniline.DefaultScanner()
		for scanner.Scan(prompt) {
			line := scanner.Text()
			if len(line) > 0 {
				scanner.AddToHistory(line)
				fmt.Println(line)
			}
		}
		if err := scanner.Err(); err != nil {
			panic(err)
		}
	}


TODO:
	Multiline
	History search
	Tab completion
	Catch SIGWINCH when window resizes

*/
package uniline
