package main

import (
	"bytes"
	"os/exec"
)

import "fmt"
import "os"

var _ = fmt.Print
var _ = os.Stdout

const (
	gitURLPre = "vagrant@flynn:"
)

func gitURL(app string) string {
	return gitURLPre + app
}

func gitRemotes(url string) (names []string) {
	out, err := exec.Command("git", "remote", "-v").Output()
	if err != nil {
		return nil
	}
	for _, line := range bytes.Split(out, []byte{'\n'}) {
		if i := bytes.IndexByte(line, '\t'); i >= 0 {
			if bytes.HasPrefix(line[i+1:], []byte(url+" ")) {
				names = append(names, string(line[:i]))
			}
		}
	}
	return names
}
