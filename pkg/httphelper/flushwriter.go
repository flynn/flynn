package httphelper

import (
	"io"
	"net/http"
)

type FlushWriter struct {
	io.Writer
	Enabled bool
}

func (f FlushWriter) Write(p []byte) (int, error) {
	if f.Enabled {
		defer func() {
			if fw, ok := f.Writer.(http.Flusher); ok {
				fw.Flush()
			}
		}()
	}
	return f.Writer.Write(p)
}
