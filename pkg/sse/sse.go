package sse

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
)

func NewWriter(w io.Writer) *Writer {
	return &Writer{w: w}
}

type Writer struct {
	w   io.Writer
	mtx sync.Mutex
}

func (w *Writer) Write(p []byte) (int, error) {
	w.mtx.Lock()
	defer w.mtx.Unlock()
	for _, line := range bytes.Split(p, []byte("\n")) {
		if _, err := fmt.Fprintf(w.w, "data: %s\n", line); err != nil {
			return 0, err
		}
	}
	// add a terminating newline
	_, err := w.w.Write([]byte("\n"))
	return len(p), err
}

func (w *Writer) Error(err error) (int, error) {
	_, e := w.w.Write([]byte("event: error\n"))
	if e != nil {
		return 0, e
	}
	return w.Write([]byte(err.Error()))
}

func (w *Writer) Flush() {
	if fw, ok := w.w.(http.Flusher); ok {
		fw.Flush()
	}
}

type Reader struct {
	*bufio.Reader
}

type Error string

func (e Error) Error() string {
	return "Server error: " + string(e)
}

func (r *Reader) Read() ([]byte, error) {
	buf := []byte{}
	var isErr bool
	for {
		line, err := r.ReadBytes('\n')
		if err != nil {
			return nil, err
		}
		if bytes.HasPrefix(line, []byte("event: error")) {
			isErr = true
		}
		if bytes.HasPrefix(line, []byte("data: ")) {
			data := bytes.TrimSuffix(bytes.TrimPrefix(line, []byte("data: ")), []byte("\n"))
			buf = append(buf, data...)
		}
		// peek ahead one byte to see if we have a double newline (terminator)
		if peek, err := r.Peek(1); err == nil && string(peek) == "\n" {
			break
		}
	}
	if isErr {
		return nil, Error(string(buf))
	}
	return buf, nil
}

type Decoder struct {
	*Reader
}

func NewDecoder(r *bufio.Reader) *Decoder {
	return &Decoder{&Reader{r}}
}

// Decode finds the next "data" field and decodes it into v
func (dec *Decoder) Decode(v interface{}) error {
	data, err := dec.Reader.Read()
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}
