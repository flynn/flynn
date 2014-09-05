package sse

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"sync"
)

type SSEWriter interface {
	Write([]byte) (int, error)
	Flush()
}

func NewSSEWriter(w io.Writer) SSEWriter {
	return &Writer{Writer: w}
}

type Writer struct {
	io.Writer
	sync.Mutex
}

func (w *Writer) Write(p []byte) (int, error) {
	w.Lock()
	defer w.Unlock()

	if _, err := w.Writer.Write([]byte("data: ")); err != nil {
		return 0, err
	}
	if _, err := w.Writer.Write(p); err != nil {
		return 0, err
	}
	_, err := w.Writer.Write([]byte("\n\n"))
	return len(p), err
}

func (w *Writer) Flush() {
	if fw, ok := w.Writer.(http.Flusher); ok {
		fw.Flush()
	}
}

type Decoder struct {
	*bufio.Reader
}

// Decode finds the next "data" field and decodes it into v
func (dec *Decoder) Decode(v interface{}) error {
	for {
		line, err := dec.ReadBytes('\n')
		if err != nil {
			return err
		}
		if bytes.HasPrefix(line, []byte("data: ")) {
			data := bytes.TrimPrefix(line, []byte("data: "))
			return json.Unmarshal(data, v)
		}
	}
}
