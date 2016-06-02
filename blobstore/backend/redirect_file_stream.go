package backend

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
)

func newRedirectFileStream(url string) FileStream {
	return &redirectFileStream{url: url}
}

type redirectFileStream struct {
	body io.ReadCloser
	url  string
}

func (s *redirectFileStream) Read(p []byte) (int, error) {
	if s.body == nil {
		res, err := http.Get(s.url)
		if err != nil {
			return 0, err
		}
		if res.StatusCode != 200 {
			res.Body.Close()
			return 0, &url.Error{
				Op:  "GET",
				URL: s.url,
				Err: fmt.Errorf("unexpected status %d", res.StatusCode),
			}
		}
		s.body = res.Body
	}
	return s.body.Read(p)
}

func (s *redirectFileStream) Close() error {
	if s.body == nil {
		return nil
	}
	return s.body.Close()
}

func (s *redirectFileStream) RedirectURL() string {
	return s.url
}

func (s *redirectFileStream) Seek(pos int64, whence int) (int64, error) {
	return 0, fmt.Errorf("blobstore: seeking a RedirectFileStream is invalid")
}
