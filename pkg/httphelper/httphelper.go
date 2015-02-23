package httphelper

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"reflect"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/julienschmidt/httprouter"
	"github.com/flynn/flynn/Godeps/_workspace/src/golang.org/x/net/context"
	"github.com/flynn/flynn/pkg/cors"
	"github.com/flynn/flynn/pkg/ctxhelper"
	"github.com/flynn/flynn/pkg/random"
)

type ErrorCode string

const (
	NotFoundError           ErrorCode = "not_found"
	ObjectNotFoundError     ErrorCode = "object_not_found"
	ObjectExistsError       ErrorCode = "object_exists"
	SyntaxError             ErrorCode = "syntax_error"
	ValidationError         ErrorCode = "validation_error"
	PreconditionFailedError ErrorCode = "precondition_failed"
	UnknownError            ErrorCode = "unknown_error"
)

var errorResponseCodes = map[ErrorCode]int{
	NotFoundError:           404,
	ObjectNotFoundError:     404,
	ObjectExistsError:       409,
	PreconditionFailedError: 412,
	SyntaxError:             400,
	ValidationError:         400,
	UnknownError:            500,
}

type JSONError struct {
	Code    ErrorCode       `json:"code"`
	Message string          `json:"message"`
	Detail  json.RawMessage `json:"detail,omitempty"`
}

func IsValidationError(err error) bool {
	e, ok := err.(JSONError)
	return ok && e.Code == ValidationError
}

var CORSAllowAllHandler = cors.Allow(&cors.Options{
	AllowAllOrigins:  true,
	AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD"},
	AllowHeaders:     []string{"Authorization", "Accept", "Content-Type", "If-Match", "If-None-Match"},
	ExposeHeaders:    []string{"ETag"},
	AllowCredentials: true,
	MaxAge:           time.Hour,
})

// Handler is an extended version of http.Handler that also takes a context
// argument ctx.
type Handler interface {
	ServeHTTP(ctx context.Context, w http.ResponseWriter, r *http.Request)
}

// The HandlerFunc type is an adapter to allow the use of ordinary functions as
// Handlers.  If f is a function with the appropriate signature, HandlerFunc(f)
// is a Handler object that calls f.
type HandlerFunc func(context.Context, http.ResponseWriter, *http.Request)

// ServeHTTP calls f(ctx, w, r).
func (f HandlerFunc) ServeHTTP(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	f(ctx, w, r)
}

func WrapHandler(handler HandlerFunc) httprouter.Handle {
	return func(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
		ctx := contextFromResponseWriter(w)
		ctx = ctxhelper.NewContextParams(ctx, params)
		handler.ServeHTTP(ctx, w, req)
	}
}

func ContextInjector(componentName string, handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		reqID := req.Header.Get("X-Request-ID")
		if reqID == "" {
			reqID = random.UUID()
		}
		ctx := ctxhelper.NewContextRequestID(context.Background(), reqID)
		ctx = ctxhelper.NewContextComponentName(ctx, componentName)
		rw := NewResponseWriter(w, ctx)
		handler.ServeHTTP(rw, req)
	})
}

func contextFromResponseWriter(w http.ResponseWriter) context.Context {
	ctx := w.(*ResponseWriter).Context()
	return ctx
}

func (jsonError JSONError) Error() string {
	return fmt.Sprintf("%s: %s", jsonError.Code, jsonError.Message)
}

func logError(w http.ResponseWriter, err error) {
	if rw, ok := w.(*ResponseWriter); ok {
		logger, _ := ctxhelper.LoggerFromContext(rw.Context())
		logger.Error(err.Error())
	} else {
		log.Println(err)
	}
}

func buildJSONError(err error) *JSONError {
	var jsonError *JSONError
	switch v := err.(type) {
	case *json.SyntaxError, *json.UnmarshalTypeError:
		jsonError = &JSONError{
			Code:    SyntaxError,
			Message: "The provided JSON input is invalid",
		}
	case JSONError:
		jsonError = &v
	case *JSONError:
		jsonError = v
	default:
		jsonError = &JSONError{
			Code:    UnknownError,
			Message: "Something went wrong",
		}
	}
	return jsonError
}

func Error(w http.ResponseWriter, err error) {
	if rw, ok := w.(*ResponseWriter); !ok || (ok && rw.Status() == 0) {
		jsonError := buildJSONError(err)
		if jsonError.Code == UnknownError {
			logError(w, err)
		}
		responseCode, ok := errorResponseCodes[jsonError.Code]
		if !ok {
			responseCode = 500
		}
		JSON(w, responseCode, jsonError)
	} else {
		logError(w, err)
	}
}

func JSON(w http.ResponseWriter, status int, v interface{}) {
	// Encode nil slices as `[]` instead of `null`
	if rv := reflect.ValueOf(v); rv.Type().Kind() == reflect.Slice && rv.IsNil() {
		v = []struct{}{}
	}

	var result []byte
	var err error
	result, err = json.Marshal(v)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(result)
}

func DecodeJSON(req *http.Request, i interface{}) error {
	dec := json.NewDecoder(req.Body)
	return dec.Decode(i)
}
