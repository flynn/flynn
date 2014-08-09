package skip32

import (
	"testing"
)

func TestSkip32(t *testing.T) {
	var in = []byte{0x33, 0x22, 0x11, 0x00}
	var key = []byte{0x00, 0x99, 0x88, 0x77, 0x66, 0x55, 0x44, 0x33, 0x22, 0x11}

	crypt32(key, in, true)

	c := (uint32(in[0]) << 24) | (uint32(in[1]) << 16) | (uint32(in[2]) << 8) | uint32(in[3])

	if c != 0x819d5f1f {
		t.Errorf("crypt32 encrypt failed: got %x expected 0x819d5f1f", c)
	}

	crypt32(key, in, false)

	c = (uint32(in[0]) << 24) | (uint32(in[1]) << 16) | (uint32(in[2]) << 8) | uint32(in[3])

	if c != 0x33221100 {
		t.Errorf("crypt32 decrypt failed: got %x expected 0x33221100", c)
	}
}

func TestObfus(t *testing.T) {

	obfu, _ := New([]byte{0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xAA})

	m := uint32(3493209676)

	c := obfu.Obfus(m)

	if c != 0x6da27100 {
		t.Errorf("id obfuscate failed: got %x expected 0x6da27100", c)
	}

	p := obfu.Unobfus(c)

	if p != 3493209676 {
		t.Errorf("id unobfuscate failed: got %x expected 3493209676", c)
	}

	c64 := obfu.Obfus64(0x0ddc0ff33badf00d)

	p64 := obfu.UnObfus64(c64)

	if p64 != 0x0ddc0ff33badf00d {
		t.Errorf("id unobfuscate failed: got %016x expected 0x0ddc0ff33badf00d", p64)
	}
}
