// +build darwin freebsd linux netbsd openbsd

package term

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
)

func IsANSI(f *os.File) bool {
	return IsTerminal(f)
}

// IsTerminal returns true if f is a terminal.
func IsTerminal(f *os.File) bool {
	cmd := exec.Command("test", "-t", "0")
	cmd.Stdin = f
	return cmd.Run() == nil
}

func MakeRaw(f *os.File) error {
	return stty(f, "-icanon", "-echo").Run()
}

func Restore(f *os.File) error {
	return stty(f, "icanon", "echo").Run()
}

func Cols() (int, error) {
	cols, err := tput("cols")
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(cols)
}

func Lines() (int, error) {
	cols, err := tput("lines")
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(cols)
}

// helpers

func stty(f *os.File, args ...string) *exec.Cmd {
	c := exec.Command("stty", args...)
	c.Stdin = f
	return c
}

func tput(what string) (string, error) {
	c := exec.Command("tput", what)
	c.Stderr = os.Stderr
	out, err := c.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
