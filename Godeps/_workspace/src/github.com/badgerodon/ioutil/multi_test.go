package ioutil

import (
	"io"
	"strings"
	"testing"
)

func TestMultiReadSeeker(t *testing.T) {
	a := "abc"
	b := "def"
	c := "ghi"

	testCases := []struct {
		offset, whence, ret int
		result              string
	}{
		{0, 0, 0, "abc"},
		{1, 0, 1, "bcd"},
		{6, 0, 6, "ghi"},
		{0, 0, 0, "abcdefghi"},
		{-2, 2, 7, "hi"},
	}

	for _, testCase := range testCases {
		rdr := NewMultiReadSeeker(strings.NewReader(a), strings.NewReader(b), strings.NewReader(c))
		for i := 0; i < 2; i++ {
			if i == 1 {
				t.Logf("Seek to 0,2")
				n, err := rdr.Seek(0, 2)
				if err != nil || n != 9 {
					t.Errorf("Expected to be able to seek to the end, got: %v, %v", n, err)
				}
			}
			t.Logf("Seek to %v,%v", testCase.offset, testCase.whence)
			ret, err := rdr.Seek(int64(testCase.offset), testCase.whence)
			if err != nil {
				t.Errorf("Expected no error got %v", err)
			}
			if ret != int64(testCase.ret) {
				t.Errorf("Expected offset of %v got %v", testCase.ret, ret)
			}
			bs := make([]byte, len(testCase.result))
			_, err = io.ReadAtLeast(rdr, bs, len(bs))
			if err != nil {
				t.Errorf("Expected no error got: %v", err)
			}
			if string(bs) != testCase.result {
				t.Errorf("Expected `%v` got `%v`", testCase.result, string(bs))
			}
		}
	}

}

func TestMultiReadSeekerReusingUnderlying(t *testing.T) {
	a := strings.NewReader("abc")
	rdr := NewMultiReadSeeker(a, a)
	bs := make([]byte, 5)
	_, err := io.ReadAtLeast(rdr, bs, 5)
	if err != nil {
		t.Errorf("Expected no error got %v", err)
	}
	if string(bs) != "abcab" {
		t.Errorf("Expected abcab got %v", string(bs))
	}
}
