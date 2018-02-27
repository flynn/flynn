package server_test

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	discoverd "github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/discoverd/server"
	"github.com/flynn/flynn/pkg/stream"
)

// Ensure the handler can register a service.
func TestHandler_PutService(t *testing.T) {
	h := NewHandler()

	// Mock the service creation.
	var called bool
	h.Store.AddServiceFn = func(service string, config *discoverd.ServiceConfig) error {
		called = true
		if service != "abc" {
			t.Fatalf("unexpected service: %s", service)
		} else if !reflect.DeepEqual(config, &discoverd.ServiceConfig{LeaderType: discoverd.LeaderTypeManual}) {
			t.Fatalf("unexpected config: %#v", config)
		}
		return nil
	}

	w := httptest.NewRecorder()
	h.ServeHTTP(w, MustNewHTTPRequest("PUT", "/services/abc", strings.NewReader(`{"leader_type":"manual"}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("unexpected status code: %d", w.Code)
	} else if !called {
		t.Fatal("Store.AddService() not called")
	}
}

// Ensure the handler returns an error if the service already exists.
func TestHandler_PutService_ErrServiceExists(t *testing.T) {
	h := NewHandler()
	h.Store.AddServiceFn = func(service string, config *discoverd.ServiceConfig) error {
		return server.ServiceExistsError("abc")
	}

	w := httptest.NewRecorder()
	h.ServeHTTP(w, MustNewHTTPRequest("PUT", "/services/abc", strings.NewReader(`{"leader_type":"manual"}`)))
	if w.Code != http.StatusConflict {
		t.Fatalf("unexpected status code: %d", w.Code)
	} else if w.Body.String() != `{"code":"object_exists","message":"discoverd: service \"abc\" already exists","retry":false}` {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
}

// Ensure the handler returns an error if the service couldn't be created.
func TestHandler_PutService_ErrUnknown(t *testing.T) {
	h := NewHandler()
	h.Store.AddServiceFn = func(service string, config *discoverd.ServiceConfig) error {
		return errors.New("marker")
	}

	w := httptest.NewRecorder()
	h.ServeHTTP(w, MustNewHTTPRequest("PUT", "/services/abc", strings.NewReader(`{"leader_type":"manual"}`)))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("unexpected status code: %d", w.Code)
	} else if w.Body.String() != `{"code":"unknown_error","message":"Something went wrong","retry":false}` {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
}

// Ensure the handler returns an error if the service has an invalid name.
func TestHandler_PutService_ErrInvalidService(t *testing.T) {
	h := NewHandler()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, MustNewHTTPRequest("PUT", "/services/XXX", strings.NewReader(`{"leader_type":"manual"}`)))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("unexpected status code: %d", w.Code)
	} else if w.Body.String() != `{"code":"validation_error","message":"discoverd: service must be lowercase alphanumeric plus dash","retry":false}` {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
}

// Ensure the handler returns an error if the body is malformed.
func TestHandler_PutService_ErrInvalidJSON(t *testing.T) {
	h := NewHandler()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, MustNewHTTPRequest("PUT", "/services/abc", strings.NewReader(`{"leader_type"`)))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("unexpected status code: %d", w.Code)
	} else if w.Body.String() != `{"code":"unknown_error","message":"Something went wrong","retry":false}` {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
}

// Ensure the handler redirects when the store is not the leader.
func TestHandler_PutService_ErrNotLeader(t *testing.T) {
	h := NewHandler()
	h.Store.LeaderFn = func() string { return "host1:1111" }
	h.Store.AddServiceFn = func(service string, config *discoverd.ServiceConfig) error { return server.ErrNotLeader }

	w := httptest.NewRecorder()
	h.ServeHTTP(w, MustNewHTTPRequest("PUT", "http://host0:1111/services/abc", strings.NewReader(`{"leader_type":"manual"}`)))
	if w.Code != http.StatusTemporaryRedirect {
		t.Fatalf("unexpected status code: %d", w.Code)
	} else if loc := w.Header().Get("Location"); loc != `http://host1:1111/services/abc` {
		t.Fatalf("unexpected Location header: %s", loc)
	}
}

// Ensure the handler can remove a service.
func TestHandler_DeleteService(t *testing.T) {
	h := NewHandler()

	// Mock the service deletion.
	var called bool
	h.Store.RemoveServiceFn = func(service string) error {
		called = true
		if service != "abc" {
			t.Fatalf("unexpected service: %s", service)
		}
		return nil
	}

	w := httptest.NewRecorder()
	h.ServeHTTP(w, MustNewHTTPRequest("DELETE", "/services/abc", strings.NewReader(`{"leader_type":"manual"}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("unexpected status code: %d", w.Code)
	} else if !called {
		t.Fatal("Store.RemoveService() not called")
	}
}

// Ensure the handler returns an error if the service has an invalid name.
func TestHandler_DeleteService_ErrInvalidService(t *testing.T) {
	h := NewHandler()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, MustNewHTTPRequest("DELETE", "/services/XXX", strings.NewReader(`{"leader_type":"manual"}`)))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("unexpected status code: %d", w.Code)
	} else if w.Body.String() != `{"code":"validation_error","message":"discoverd: service must be lowercase alphanumeric plus dash","retry":false}` {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
}

// Ensure the handler returns an error if the service is not found.
func TestHandler_DeleteService_ErrNotFound(t *testing.T) {
	h := NewHandler()
	h.Store.RemoveServiceFn = func(service string) error {
		return server.NotFoundError{Service: "abc"}
	}

	w := httptest.NewRecorder()
	h.ServeHTTP(w, MustNewHTTPRequest("DELETE", "/services/abc", strings.NewReader(`{"leader_type":"manual"}`)))
	if w.Code != http.StatusNotFound {
		t.Fatalf("unexpected status code: %d", w.Code)
	} else if w.Body.String() != `{"code":"object_not_found","message":"discoverd: service \"abc\" not found","retry":false}` {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
}

// Ensure the handler returns an error if the service couldn't be removed.
func TestHandler_DeleteService_ErrUnknown(t *testing.T) {
	h := NewHandler()
	h.Store.RemoveServiceFn = func(service string) error {
		return errors.New("marker")
	}

	w := httptest.NewRecorder()
	h.ServeHTTP(w, MustNewHTTPRequest("DELETE", "/services/abc", strings.NewReader(`{"leader_type":"manual"}`)))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("unexpected status code: %d", w.Code)
	} else if w.Body.String() != `{"code":"unknown_error","message":"Something went wrong","retry":false}` {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
}

// Ensure the handler can stream events from a service.
func TestHandler_GetService_Stream(t *testing.T) {
	h := NewHandler()
	h.Store.SubscribeFn = func(service string, sendCurrent bool, kinds discoverd.EventKind, ch chan *discoverd.Event) stream.Stream {
		if service != "abc" {
			t.Fatalf("unexpected service: %s", service)
		} else if sendCurrent != true {
			t.Fatalf("unexpected send current: %v", sendCurrent)
		} else if kinds != discoverd.EventKindAll {
			t.Fatalf("unexpected kinds: %d", kinds)
		}

		// Send an event back to the stream.
		ch <- &discoverd.Event{
			Service: service,
			Kind:    discoverd.EventKindLeader,
		}
		close(ch)
		return chanStream(ch)
	}

	w := httptest.NewRecorder()
	r := MustNewHTTPRequest("GET", "/services/abc", nil)
	r.Header.Set("Accept", "text/event-stream")
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("unexpected status code: %d", w.Code)
	} else if w.Body.String() != `data: {"service":"abc","kind":"leader"}`+"\n\n" {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
}

// Ensure the handler can return errors from a closing stream.
func TestHandler_GetService_Stream_ErrStream(t *testing.T) {
	h := NewHandler()
	h.Store.SubscribeFn = func(_ string, _ bool, _ discoverd.EventKind, ch chan *discoverd.Event) stream.Stream {
		close(ch)
		return erroringChanStream(ch)
	}

	w := httptest.NewRecorder()
	r := MustNewHTTPRequest("GET", "/services/abc", nil)
	r.Header.Set("Accept", "text/event-stream")
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("unexpected status code: %d", w.Code)
	} else if w.Body.String() != "event: error\ndata: stream marker error\n\n" {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
}

// Ensure the handler can set metadata for a service.
func TestHandler_PutServiceMeta(t *testing.T) {
	h := NewHandler()

	// Mock the store meta assignment.
	var called bool
	h.Store.SetServiceMetaFn = func(service string, meta *discoverd.ServiceMeta) error {
		called = true
		if service != "abc" {
			t.Fatalf("unexpected service: %s", service)
		} else if !reflect.DeepEqual(meta, &discoverd.ServiceMeta{Index: 12, Data: json.RawMessage(`{"foo":"bar"}`)}) {
			t.Fatalf("unexpected meta: %#v", meta)
		}
		return nil
	}

	w := httptest.NewRecorder()
	h.ServeHTTP(w, MustNewHTTPRequest("PUT", "/services/abc/meta", strings.NewReader(`{"index":12,"data":{"foo":"bar"}}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("unexpected status code: %d", w.Code)
	} else if !called {
		t.Fatal("Store.SetServiceMeta() not called")
	} else if w.Body.String() != `{"data":{"foo":"bar"},"index":12}` {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
}

// Ensure the handler returns an error if invalid JSON is passed in.
func TestHandler_PutServiceMeta_ErrInvalidJSON(t *testing.T) {
	h := NewHandler()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, MustNewHTTPRequest("PUT", "/services/abc/meta", strings.NewReader(`{"index":`)))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("unexpected status code: %d", w.Code)
	} else if w.Body.String() != `{"code":"unknown_error","message":"Something went wrong","retry":false}` {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
}

// Ensure the handler returns an error if the service isn't found.
func TestHandler_PutServiceMeta_ErrNotFound(t *testing.T) {
	h := NewHandler()
	h.Store.SetServiceMetaFn = func(_ string, _ *discoverd.ServiceMeta) error {
		return server.NotFoundError{Service: "abc"}
	}

	w := httptest.NewRecorder()
	h.ServeHTTP(w, MustNewHTTPRequest("PUT", "/services/abc/meta", strings.NewReader(`{"index":12}`)))
	if w.Code != http.StatusNotFound {
		t.Fatalf("unexpected status code: %d", w.Code)
	} else if w.Body.String() != `{"code":"object_not_found","message":"discoverd: service \"abc\" not found","retry":false}` {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
}

// Ensure the handler returns an error if the meta cannot be set.
func TestHandler_PutServiceMeta_ErrUnknown(t *testing.T) {
	h := NewHandler()
	h.Store.SetServiceMetaFn = func(_ string, _ *discoverd.ServiceMeta) error {
		return errors.New("marker")
	}

	w := httptest.NewRecorder()
	h.ServeHTTP(w, MustNewHTTPRequest("PUT", "/services/abc/meta", strings.NewReader(`{"index":12}`)))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("unexpected status code: %d", w.Code)
	} else if w.Body.String() != `{"code":"unknown_error","message":"Something went wrong","retry":false}` {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
}

// Ensure the handler can retrieve metadata for a service.
func TestHandler_GetServiceMeta(t *testing.T) {
	h := NewHandler()
	h.Store.ServiceMetaFn = func(service string) *discoverd.ServiceMeta {
		if service != "abc" {
			t.Fatalf("unexpected service: %s", service)
		}
		return &discoverd.ServiceMeta{Index: 12, Data: json.RawMessage(`{"foo":"bar"}`)}
	}

	w := httptest.NewRecorder()
	h.ServeHTTP(w, MustNewHTTPRequest("GET", "/services/abc/meta", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("unexpected status code: %d", w.Code)
	} else if w.Body.String() != `{"data":{"foo":"bar"},"index":12}` {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
}

// Ensure the handler returns an error if the service cannot be found.
func TestHandler_GetServiceMeta_ErrNotFound(t *testing.T) {
	h := NewHandler()
	h.Store.ServiceMetaFn = func(service string) *discoverd.ServiceMeta { return nil }

	w := httptest.NewRecorder()
	h.ServeHTTP(w, MustNewHTTPRequest("GET", "/services/abc/meta", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("unexpected status code: %d", w.Code)
	} else if w.Body.String() != `{"code":"object_not_found","message":"service meta not found","retry":false}` {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
}

// Ensure the handler can create an instance for a service.
func TestHandler_PutInstance(t *testing.T) {
	h := NewHandler()

	// Mock the instance creation.
	var called bool
	h.Store.AddInstanceFn = func(service string, inst *discoverd.Instance) error {
		called = true
		if service != "abc" {
			t.Fatalf("unexpected service: %s", service)
		} else if !reflect.DeepEqual(inst, &discoverd.Instance{
			ID:    "74667cebd845d088d811ddef924895b7",
			Addr:  "localhost:10000",
			Proto: "http",
			Meta:  map[string]string{"foo": "bar"},
			Index: 12,
		}) {
			t.Fatalf("unexpected inst: %#v", inst)
		}
		return nil
	}

	w := httptest.NewRecorder()
	h.ServeHTTP(w, MustNewHTTPRequest("PUT", "/services/abc/instances/74667cebd845d088d811ddef924895b7",
		strings.NewReader(`{"id":"74667cebd845d088d811ddef924895b7","addr":"localhost:10000","proto":"http","meta":{"foo":"bar"},"index":12}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("unexpected status code: %d", w.Code)
	} else if w.Body.String() != `` {
		t.Fatalf("unexpected body: %s", w.Body.String())
	} else if !called {
		t.Fatal("Store.AddInstance() not called")
	}
}

// Ensure the handler returns an error if body cannot be parsed.
func TestHandler_PutInstance_ErrInvalidJSON(t *testing.T) {
	h := NewHandler()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, MustNewHTTPRequest("PUT", "/services/abc/instances/74667cebd845d088d811ddef924895b7", strings.NewReader(`{"id":`)))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("unexpected status code: %d", w.Code)
	} else if w.Body.String() != `{"code":"unknown_error","message":"Something went wrong","retry":false}` {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
}

// Ensure the handler returns an error if the instance is invalid.
func TestHandler_PutInstance_ErrInvalid(t *testing.T) {
	h := NewHandler()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, MustNewHTTPRequest("PUT", "/services/abc/instances/xxx",
		strings.NewReader(`{"id":"xxx"}`)))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("unexpected status code: %d", w.Code)
	} else if w.Body.String() != `{"code":"validation_error","message":"discoverd: proto must be set","retry":false}` {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
}

// Ensure the handler returns an error if the service is not found.
func TestHandler_PutInstance_ErrNotFound(t *testing.T) {
	h := NewHandler()
	h.Store.AddInstanceFn = func(service string, inst *discoverd.Instance) error {
		return server.NotFoundError{Service: "abc"}
	}

	w := httptest.NewRecorder()
	h.ServeHTTP(w, MustNewHTTPRequest("PUT", "/services/abc/instances/xxx",
		strings.NewReader(`{"id":"74667cebd845d088d811ddef924895b7","addr":"localhost:10000","proto":"http","meta":{"foo":"bar"},"index":12}`)))
	if w.Code != http.StatusNotFound {
		t.Fatalf("unexpected status code: %d", w.Code)
	} else if w.Body.String() != `{"code":"object_not_found","message":"discoverd: service \"abc\" not found","retry":false}` {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
}

// Ensure the handler returns an error if the instance cannot be created.
func TestHandler_PutInstance_ErrUnknown(t *testing.T) {
	h := NewHandler()
	h.Store.AddInstanceFn = func(service string, inst *discoverd.Instance) error {
		return errors.New("marker")
	}

	w := httptest.NewRecorder()
	h.ServeHTTP(w, MustNewHTTPRequest("PUT", "/services/abc/instances/xxx",
		strings.NewReader(`{"id":"74667cebd845d088d811ddef924895b7","addr":"localhost:10000","proto":"http","meta":{"foo":"bar"},"index":12}`)))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("unexpected status code: %d", w.Code)
	} else if w.Body.String() != `{"code":"unknown_error","message":"Something went wrong","retry":false}` {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
}

// Ensure the handler can remove an instance.
func TestHandler_DeleteInstance(t *testing.T) {
	h := NewHandler()

	// Mock the instance deletion.
	var called bool
	h.Store.RemoveInstanceFn = func(service, instanceID string) error {
		called = true
		if service != "abc" {
			t.Fatalf("unexpected service: %s", service)
		} else if instanceID != "74667cebd845d088d811ddef924895b7" {
			t.Fatalf("unexpected instance id: %#v", instanceID)
		}
		return nil
	}

	w := httptest.NewRecorder()
	h.ServeHTTP(w, MustNewHTTPRequest("DELETE", "/services/abc/instances/74667cebd845d088d811ddef924895b7", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("unexpected status code: %d", w.Code)
	} else if w.Body.String() != `` {
		t.Fatalf("unexpected body: %s", w.Body.String())
	} else if !called {
		t.Fatal("Store.RemoveInstance() not called")
	}
}

// Ensure the handler returns an error if the instance doesn't exist.
func TestHandler_DeleteInstance_ErrNotFound(t *testing.T) {
	h := NewHandler()
	h.Store.RemoveInstanceFn = func(service, instanceID string) error {
		return server.NotFoundError{Service: "abc", Instance: "74667cebd845d088d811ddef924895b7"}
	}

	w := httptest.NewRecorder()
	h.ServeHTTP(w, MustNewHTTPRequest("DELETE", "/services/abc/instances/74667cebd845d088d811ddef924895b7", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("unexpected status code: %d", w.Code)
	} else if w.Body.String() != `{"code":"object_not_found","message":"discoverd: instance abc/74667cebd845d088d811ddef924895b7 not found","retry":false}` {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
}

// Ensure the handler returns an error if the instance can't be removed.
func TestHandler_DeleteInstance_ErrUnknown(t *testing.T) {
	h := NewHandler()
	h.Store.RemoveInstanceFn = func(service, instanceID string) error { return errors.New("marker") }

	w := httptest.NewRecorder()
	h.ServeHTTP(w, MustNewHTTPRequest("DELETE", "/services/abc/instances/74667cebd845d088d811ddef924895b7", nil))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("unexpected status code: %d", w.Code)
	} else if w.Body.String() != `{"code":"unknown_error","message":"Something went wrong","retry":false}` {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
}

// Ensure the handler can retrieve a list of instances for a service.
func TestHandler_GetInstances(t *testing.T) {
	h := NewHandler()
	h.Store.InstancesFn = func(service string) ([]*discoverd.Instance, error) {
		return []*discoverd.Instance{{ID: "inst0"}, {ID: "inst1"}}, nil
	}

	w := httptest.NewRecorder()
	h.ServeHTTP(w, MustNewHTTPRequest("GET", "/services/abc/instances", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("unexpected status code: %d", w.Code)
	} else if w.Body.String() != `[{"id":"inst0","addr":"","proto":""},{"id":"inst1","addr":"","proto":""}]` {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
}

// Ensure the handler can stream instances events from a service.
func TestHandler_GetInstances_Stream(t *testing.T) {
	h := NewHandler()
	h.Store.SubscribeFn = func(service string, sendCurrent bool, kinds discoverd.EventKind, ch chan *discoverd.Event) stream.Stream {
		if service != "abc" {
			t.Fatalf("unexpected service: %s", service)
		} else if sendCurrent != true {
			t.Fatalf("unexpected send current: %v", sendCurrent)
		} else if kinds != discoverd.EventKindUp|discoverd.EventKindUpdate|discoverd.EventKindDown {
			t.Fatalf("unexpected kinds: %d", kinds)
		}

		// Send an event back to the stream.
		ch <- &discoverd.Event{
			Service:  service,
			Kind:     discoverd.EventKindUp,
			Instance: &discoverd.Instance{ID: "xxx"},
		}
		close(ch)
		return chanStream(ch)
	}

	w := httptest.NewRecorder()
	r := MustNewHTTPRequest("GET", "/services/abc/instances", nil)
	r.Header.Set("Accept", "text/event-stream")
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("unexpected status code: %d", w.Code)
	} else if w.Body.String() != `data: {"service":"abc","kind":"up","instance":{"id":"xxx","addr":"","proto":""}}`+"\n\n" {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
}

// Ensure the handler returns an error if a nil set of instances is returned from the store.
func TestHandler_GetInstances_ErrNotFound(t *testing.T) {
	h := NewHandler()
	h.Store.InstancesFn = func(service string) ([]*discoverd.Instance, error) { return nil, nil }

	w := httptest.NewRecorder()
	h.ServeHTTP(w, MustNewHTTPRequest("GET", "/services/abc/instances", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("unexpected status code: %d", w.Code)
	} else if w.Body.String() != `{"code":"object_not_found","message":"service not found: \"abc\"","retry":false}` {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
}

// Ensure the handler can set the leader for a service.
func TestHandler_PutLeader(t *testing.T) {
	h := NewHandler()

	// Mock the config & leader assignment.
	h.Store.ConfigFn = func(service string) *discoverd.ServiceConfig {
		if service != "abc" {
			t.Fatalf("config: unexpected service: %s", service)
		}
		return &discoverd.ServiceConfig{LeaderType: discoverd.LeaderTypeManual}
	}
	h.Store.SetServiceLeaderFn = func(service, instanceID string) error {
		if service != "abc" {
			t.Fatalf("set leader: unexpected service: %s", service)
		} else if instanceID != "xxx" {
			t.Fatalf("set leader: unexpected instance id: %s", instanceID)
		}
		return nil
	}

	w := httptest.NewRecorder()
	h.ServeHTTP(w, MustNewHTTPRequest("PUT", "/services/abc/leader", strings.NewReader(`{"id":"xxx"}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("unexpected status code: %d", w.Code)
	} else if w.Body.String() != `` {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
}

// Ensure the handler returns an error if no service configuration is set.
func TestHandler_PutLeader_ErrConfigNotFound(t *testing.T) {
	h := NewHandler()
	h.Store.ConfigFn = func(service string) *discoverd.ServiceConfig { return nil }

	w := httptest.NewRecorder()
	h.ServeHTTP(w, MustNewHTTPRequest("PUT", "/services/abc/leader", strings.NewReader(`{"id":"xxx"}`)))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("unexpected status code: %d", w.Code)
	} else if w.Body.String() != `{"code":"validation_error","message":"service leader election type is not manual","retry":false}` {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
}

// Ensure the handler returns an error if the service does not have manual election.
func TestHandler_PutLeader_ErrNotManual(t *testing.T) {
	h := NewHandler()
	h.Store.ConfigFn = func(service string) *discoverd.ServiceConfig {
		return &discoverd.ServiceConfig{LeaderType: discoverd.LeaderTypeOldest}
	}

	w := httptest.NewRecorder()
	h.ServeHTTP(w, MustNewHTTPRequest("PUT", "/services/abc/leader", strings.NewReader(`{"id":"xxx"}`)))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("unexpected status code: %d", w.Code)
	} else if w.Body.String() != `{"code":"validation_error","message":"service leader election type is not manual","retry":false}` {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
}

// Ensure the handler returns an error if the request body contains invalid JSON.
func TestHandler_PutLeader_ErrInvalidJSON(t *testing.T) {
	h := NewHandler()
	h.Store.ConfigFn = func(service string) *discoverd.ServiceConfig {
		return &discoverd.ServiceConfig{LeaderType: discoverd.LeaderTypeManual}
	}

	w := httptest.NewRecorder()
	h.ServeHTTP(w, MustNewHTTPRequest("PUT", "/services/abc/leader", strings.NewReader(`{"id":`)))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("unexpected status code: %d", w.Code)
	} else if w.Body.String() != `{"code":"unknown_error","message":"Something went wrong","retry":false}` {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
}

// Ensure the handler returns an error if the leader could not be set.
func TestHandler_PutLeader_ErrUnknown(t *testing.T) {
	h := NewHandler()
	h.Store.ConfigFn = func(service string) *discoverd.ServiceConfig {
		return &discoverd.ServiceConfig{LeaderType: discoverd.LeaderTypeManual}
	}
	h.Store.SetServiceLeaderFn = func(service, instanceID string) error { return errors.New("marker") }

	w := httptest.NewRecorder()
	h.ServeHTTP(w, MustNewHTTPRequest("PUT", "/services/abc/leader", strings.NewReader(`{"id":"xxx"}`)))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("unexpected status code: %d", w.Code)
	} else if w.Body.String() != `{"code":"unknown_error","message":"Something went wrong","retry":false}` {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
}

// Ensure the handler can retrieve the current leader for a service.
func TestHandler_GetLeader(t *testing.T) {
	h := NewHandler()
	h.Store.ServiceLeaderFn = func(service string) (*discoverd.Instance, error) {
		if service != "abc" {
			t.Fatalf("unexpected service: %s", service)
		}
		return &discoverd.Instance{ID: "xxx"}, nil
	}

	w := httptest.NewRecorder()
	h.ServeHTTP(w, MustNewHTTPRequest("GET", "/services/abc/leader", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("unexpected status code: %d", w.Code)
	} else if w.Body.String() != `{"id":"xxx","addr":"","proto":""}` {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
}

// Ensure the handler can stream leader events from a service.
func TestHandler_GetLeader_Stream(t *testing.T) {
	h := NewHandler()
	h.Store.SubscribeFn = func(service string, sendCurrent bool, kinds discoverd.EventKind, ch chan *discoverd.Event) stream.Stream {
		if service != "abc" {
			t.Fatalf("unexpected service: %s", service)
		} else if sendCurrent != true {
			t.Fatalf("unexpected send current: %v", sendCurrent)
		} else if kinds != discoverd.EventKindLeader {
			t.Fatalf("unexpected kinds: %d", kinds)
		}

		// Send an event back to the stream.
		ch <- &discoverd.Event{
			Service:  service,
			Kind:     discoverd.EventKindLeader,
			Instance: &discoverd.Instance{ID: "xxx"},
		}
		close(ch)
		return chanStream(ch)
	}

	w := httptest.NewRecorder()
	r := MustNewHTTPRequest("GET", "/services/abc/leader", nil)
	r.Header.Set("Accept", "text/event-stream")
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("unexpected status code: %d", w.Code)
	} else if w.Body.String() != `data: {"service":"abc","kind":"leader","instance":{"id":"xxx","addr":"","proto":""}}`+"\n\n" {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
}

// Ensure the handler returns an error if there is not a current leader.
func TestHandler_GetLeader_ErrNoLeader(t *testing.T) {
	h := NewHandler()
	h.Store.ServiceLeaderFn = func(service string) (*discoverd.Instance, error) { return nil, nil }

	w := httptest.NewRecorder()
	h.ServeHTTP(w, MustNewHTTPRequest("GET", "/services/abc/leader", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("unexpected status code: %d", w.Code)
	} else if w.Body.String() != `{"code":"object_not_found","message":"no leader found","retry":false}` {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
}

// Handler represents a test wrapper for server.Handler.
type Handler struct {
	*server.Handler
	Store MockStore
}

// NewHandler returns a new, mocked instance Handler.
func NewHandler() *Handler {
	h := &Handler{Handler: server.NewHandler(false, []string{""})}
	h.Handler.Store = &h.Store
	h.Store.IsLeaderFn = func() bool { return true }
	h.Store.GetPeersFn = func() ([]string, error) {
		return []string{""}, nil
	}
	h.Store.LastIndexFn = func() uint64 { return 0 }
	return h
}

// MustNewHTTPRequest returns a new HTTP request. Panic on error.
func MustNewHTTPRequest(method, urlStr string, body io.Reader) *http.Request {
	u, err := url.Parse(urlStr)
	if err != nil {
		panic(err)
	}

	req, err := http.NewRequest(method, urlStr, body)
	if err != nil {
		panic(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	// Set host
	req.Host = u.Host

	return req
}

// chanStream implements stream.Stream for an event channel.
type chanStream chan *discoverd.Event

func (chanStream) Err() error   { return nil }
func (chanStream) Close() error { return nil }

// erroringChanStream implements stream.Stream for an event channel.
// Always returns an error from Err().
type erroringChanStream chan *discoverd.Event

func (erroringChanStream) Err() error   { return errors.New("stream marker error") }
func (erroringChanStream) Close() error { return nil }
