package pq

import (
	"io"
	"testing"
)

func TestLargeObject(t *testing.T) {
	db := openTestConn(t)

	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}

	lo, err := NewLargeObjects(tx)
	if err != nil {
		t.Fatal(err)
	}

	id, err := lo.Create(0)
	if err != nil {
		t.Fatal(err)
	}

	obj, err := lo.Open(id, LargeObjectModeRead|LargeObjectModeWrite)
	if err != nil {
		t.Fatal(err)
	}

	n, err := obj.Write([]byte("testing"))
	if err != nil {
		t.Fatal(err)
	}
	if n != 7 {
		t.Errorf("Expected n to be 7, got %d", n)
	}

	pos, err := obj.Seek(1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if pos != 1 {
		t.Errorf("Expected pos to be 1, got %d", pos)
	}

	res := make([]byte, 6)
	n, err = obj.Read(res)
	if err != nil {
		t.Fatal(err)
	}
	if string(res) != "esting" {
		t.Errorf(`Expected res to be "esting", got %q`, res)
	}
	if n != 6 {
		t.Errorf("Expected n to be 6, got %d", n)
	}

	n, err = obj.Read(res)
	if err != io.EOF {
		t.Error("Expected io.EOF, go nil")
	}
	if n != 0 {
		t.Errorf("Expected n to be 0, got %d", n)
	}

	pos, err = obj.Tell()
	if err != nil {
		t.Fatal(err)
	}
	if pos != 7 {
		t.Errorf("Expected pos to be 7, got %d", pos)
	}

	err = obj.Truncate(1)
	if err != nil {
		t.Fatal(err)
	}

	pos, err = obj.Seek(-1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if pos != 0 {
		t.Errorf("Expected pos to be 0, got %d", pos)
	}

	res = make([]byte, 2)
	n, err = obj.Read(res)
	if err != io.EOF {
		t.Errorf("Expected err to be io.EOF, got %v", err)
	}
	if n != 1 {
		t.Errorf("Expected n to be 1, got %d", n)
	}
	if res[0] != 't' {
		t.Errorf("Expected res[0] to be 'T', got %v", res[0])
	}

	err = obj.Close()
	if err != nil {
		t.Fatal(err)
	}

	err = lo.Unlink(id)
	if err != nil {
		t.Fatal(err)
	}

	_, err = lo.Open(id, LargeObjectModeRead)
	if e, ok := err.(*Error); !ok || e.Code.Name() != "undefined_object" {
		t.Errorf("Expected undefined_object error, got %#v", err)
	}
}
