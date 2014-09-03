package ioutil

import (
	"bytes"
	"io/ioutil"
	"testing"
)

func TestSectionReader(t *testing.T) {
	original := bytes.NewReader([]byte("Hello World"))
	rdr := NewSectionReader(original, 2, 3)

	bs, err := ioutil.ReadAll(rdr)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if string(bs) != "llo" {
		t.Fatalf("Expected `llo` got: `%v`", string(bs))
	}
}
