package discoverd

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClientMaintainsHeadersOnRedirect(t *testing.T) {
	errc := make(chan error, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/services/test/leader", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "text/event-stream" {
			errc <- errors.New("Accept header was not forwarded")
			return
		}
		errc <- nil
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	rs := httptest.NewServer(http.RedirectHandler(ts.URL+"/services/test/leader", http.StatusFound))
	defer rs.Close()

	client := NewClientWithURL(rs.URL)

	leaders := make(chan *Instance)
	stream, err := client.Service("test").Leaders(leaders)
	if err != nil {
		t.Fatal(err)
	}
	stream.Close()
	select {
	case err = <-errc:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("response timeout")
	}
}
