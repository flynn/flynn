package httphelper

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/julienschmidt/httprouter"
	"github.com/flynn/flynn/Godeps/_workspace/src/golang.org/x/net/context"
	"github.com/flynn/flynn/pkg/cors"
	"github.com/flynn/flynn/pkg/random"
)

type ErrorCode string

const (
	NotFoundError       ErrorCode = "not_found"
	ObjectNotFoundError           = "object_not_found"
	ObjectExistsError             = "object_exists"
	SyntaxError                   = "syntax_error"
	ValidationError               = "validation_error"
	UnknownError                  = "unknown_error"
)

var errorResponseCodes = map[ErrorCode]int{
	NotFoundError:       404,
	ObjectNotFoundError: 404,
	ObjectExistsError:   409,
	SyntaxError:         400,
	ValidationError:     400,
	UnknownError:        500,
}

type JSONError struct {
	Code    ErrorCode       `json:"code"`
	Message string          `json:"message"`
	Detail  json.RawMessage `json:"detail,omitempty"`
}

var CORSAllowAllHandler = cors.Allow(&cors.Options{
	AllowAllOrigins:  true,
	AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD"},
	AllowHeaders:     []string{"Authorization", "Accept", "Content-Type", "If-Match", "If-None-Match"},
	ExposeHeaders:    []string{"ETag"},
	AllowCredentials: true,
	MaxAge:           time.Hour,
})

type CtxKey string

const (
	CtxKeyComponent CtxKey = "component"
	CtxKeyReqID            = "req_id"
	CtxKeyParams           = "params"
)

type Handle func(context.Context, http.ResponseWriter, *http.Request)

func WrapHandler(handler Handle) httprouter.Handle {
	return func(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
		ctx := contextFromResponseWriter(w)
		ctx = context.WithValue(ctx, CtxKeyParams, params)
		handler(ctx, w, req)
	}
}

func ContextInjector(componentName string, handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		reqID := req.Header.Get("X-Request-ID")
		if reqID == "" {
			reqID = random.UUID()
		}
		ctx := context.WithValue(context.Background(), CtxKeyReqID, reqID)
		ctx = context.WithValue(ctx, CtxKeyComponent, componentName)
		rw := NewResponseWriter(w)
		rw.ctx = ctx
		handler.ServeHTTP(rw, req)
	})
}

func ParamsFromContext(ctx context.Context) httprouter.Params {
	params := ctx.Value(CtxKeyParams).(httprouter.Params)
	return params
}

func contextFromResponseWriter(w http.ResponseWriter) context.Context {
	ctx := w.(*ResponseWriter).Context()
	return ctx
}

func (jsonError JSONError) Error() string {
	return fmt.Sprintf("%s: %s", jsonError.Code, jsonError.Message)
}

func Error(w http.ResponseWriter, err error) {
	var jsonError JSONError
	switch err.(type) {
	case *json.SyntaxError, *json.UnmarshalTypeError:
		jsonError = JSONError{
			Code:    SyntaxError,
			Message: "The provided JSON input is invalid",
		}
	case JSONError:
		jsonError = err.(JSONError)
	case *JSONError:
		jsonError = *err.(*JSONError)
	default:
		log.Println(err)
		jsonError = JSONError{
			Code:    UnknownError,
			Message: "Something went wrong",
		}
	}

	responseCode, ok := errorResponseCodes[jsonError.Code]
	if !ok {
		responseCode = 500
	}
	JSON(w, responseCode, jsonError)
}

func JSON(w http.ResponseWriter, status int, v interface{}) {
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
