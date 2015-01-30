package ctxhelper

import (
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/julienschmidt/httprouter"
	"github.com/flynn/flynn/Godeps/_workspace/src/golang.org/x/net/context"
	log "github.com/flynn/flynn/Godeps/_workspace/src/gopkg.in/inconshreveable/log15.v2"
)

type ctxKey int

const (
	ctxKeyComponent ctxKey = iota
	ctxKeyReqID
	ctxKeyParams
	ctxKeyLogger
)

func NewContextComponentName(ctx context.Context, componentName string) context.Context {
	return context.WithValue(ctx, ctxKeyComponent, componentName)
}

func ComponentNameFromContext(ctx context.Context) (componentName string, ok bool) {
	componentName, ok = ctx.Value(ctxKeyComponent).(string)
	return
}

func NewContextLogger(ctx context.Context, logger log.Logger) context.Context {
	return context.WithValue(ctx, ctxKeyLogger, logger)
}

func LoggerFromContext(ctx context.Context) (logger log.Logger, ok bool) {
	logger, ok = ctx.Value(ctxKeyLogger).(log.Logger)
	return
}

func NewContextParams(ctx context.Context, params httprouter.Params) context.Context {
	return context.WithValue(ctx, ctxKeyParams, params)
}

func ParamsFromContext(ctx context.Context) (params httprouter.Params, ok bool) {
	params, ok = ctx.Value(ctxKeyParams).(httprouter.Params)
	return
}

func NewContextRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxKeyReqID, id)
}

func RequestIDFromContext(ctx context.Context) (id string, ok bool) {
	id, ok = ctx.Value(ctxKeyReqID).(string)
	return
}
