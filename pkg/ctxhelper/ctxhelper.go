package ctxhelper

import (
	"time"

	"github.com/julienschmidt/httprouter"
	"golang.org/x/net/context"
	log "gopkg.in/inconshreveable/log15.v2"
)

type ctxKey int

const (
	ctxKeyComponent ctxKey = iota
	ctxKeyLogger
	ctxKeyParams
	ctxKeyReqID
	ctxKeyStartTime
)

// NewContextComponentName creates a new context that carries the provided
// componentName value.
func NewContextComponentName(ctx context.Context, componentName string) context.Context {
	return context.WithValue(ctx, ctxKeyComponent, componentName)
}

// ComponentNameFromContext extracts a component name from a context.
func ComponentNameFromContext(ctx context.Context) (componentName string, ok bool) {
	componentName, ok = ctx.Value(ctxKeyComponent).(string)
	return
}

// NewContextLogger creates a new context that carries the provided logger
// value.
func NewContextLogger(ctx context.Context, logger log.Logger) context.Context {
	return context.WithValue(ctx, ctxKeyLogger, logger)
}

// LoggerFromContext extracts a logger from a context.
func LoggerFromContext(ctx context.Context) (logger log.Logger, ok bool) {
	logger, ok = ctx.Value(ctxKeyLogger).(log.Logger)
	return
}

// NewContextParams creates a new context that carries the provided params
// value.
func NewContextParams(ctx context.Context, params httprouter.Params) context.Context {
	return context.WithValue(ctx, ctxKeyParams, params)
}

// ParamsFromContext extracts params from a context.
func ParamsFromContext(ctx context.Context) (params httprouter.Params, ok bool) {
	params, ok = ctx.Value(ctxKeyParams).(httprouter.Params)
	return
}

// NewContextRequestID creates a new context that carries the provided request
// ID value.
func NewContextRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxKeyReqID, id)
}

// RequestIDFromContext extracts a request ID from a context.
func RequestIDFromContext(ctx context.Context) (id string, ok bool) {
	id, ok = ctx.Value(ctxKeyReqID).(string)
	return
}

// NewContextStartTime creates a new context that carries the provided start
// time.
func NewContextStartTime(ctx context.Context, start time.Time) context.Context {
	return context.WithValue(ctx, ctxKeyStartTime, start)
}

// StartTimeFromContext extracts a start time from a context.
func StartTimeFromContext(ctx context.Context) (start time.Time, ok bool) {
	start, ok = ctx.Value(ctxKeyStartTime).(time.Time)
	return
}
