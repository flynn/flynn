package main_test

import (
	"bytes"
	"io"
	"os"
	"testing"

	main "github.com/flynn/flynn/appliance/redis/cmd/flynn-redis-api"
)

// Main is a test wrapper for main.Main.
type Main struct {
	*main.Main

	Stdin  bytes.Buffer
	Stdout bytes.Buffer
	Stderr bytes.Buffer
}

// NewMain returns a new instance of Main.
func NewMain() *Main {
	m := &Main{
		Main: main.NewMain(),
	}
	m.Main.Addr = "127.0.0.1:0"

	m.Main.Stdin = &m.Stdin
	m.Main.Stdout = &m.Stdout
	m.Main.Stderr = &m.Stderr

	if testing.Verbose() {
		m.Main.Stdout = io.MultiWriter(os.Stdout, m.Main.Stdout)
		m.Main.Stderr = io.MultiWriter(os.Stderr, m.Main.Stderr)
	}

	return m
}

// URL returns the string URL for communicating with the program's HTTP handler.
func (m *Main) URL() string { return "http://" + m.Listener().Addr().String() }
