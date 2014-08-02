package pinned

import (
	"encoding/hex"
	"net/http/httptest"
	"testing"
)

func TestPin(t *testing.T) {
	srv := httptest.NewTLSServer(nil)
	addr := srv.Listener.Addr().String()

	pin, _ := hex.DecodeString("6c2896594d2432e030c75fbc36b57a69820bc36a6a064430a561e6b53483607a")
	config := &Config{Pin: pin}
	conn, err := config.Dial("tcp", addr)
	if err != nil {
		t.Error(err)
	}
	conn.Close()

	config.Pin[0] = 0
	conn, err = config.Dial("tcp", addr)
	if err != ErrPinFailure || conn != nil {
		t.Errorf("Expected to get (nil, ErrPinFailure), got (%v, %v)", conn, err)
	}
}
