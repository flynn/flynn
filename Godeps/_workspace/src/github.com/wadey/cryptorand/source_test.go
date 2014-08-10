package cryptorand_test

import (
	"bytes"
	"testing"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/wadey/cryptorand"
)

func TestSource(t *testing.T) {
	s := cryptorand.Source
	if s.Int63() == s.Int63() {
		t.Error("Expected Int63() to be random")
	}
}

func TestNewSource(t *testing.T) {
	b := bytes.NewReader(make([]byte, 8))
	s := cryptorand.NewSource(b)
	if s.Int63() != 0 {
		t.Error("Expected Int63() to be 0 with custom io.Reader")
	}

	b = bytes.NewReader([]byte{0x7f, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff})
	s = cryptorand.NewSource(b)
	if s.Int63() != 1<<63-1 {
		t.Error("Expected Int63() to be max with custom io.Reader")
	}
}

func TestSeedPanics(t *testing.T) {
	defer func() {
		if err := recover(); err == nil {
			t.Error("Expected Seed() to panic")
		}
	}()
	s := cryptorand.Source
	s.Seed(1)
}

func BenchmarkRandSource(b *testing.B) {
	s := cryptorand.Source
	b.SetBytes(8)
	for i := 0; i < b.N; i++ {
		s.Int63()
	}
}
