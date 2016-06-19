package main

import (
	"net/http"
	"reflect"

	"github.com/flynn/flynn/controller/schema"
	"github.com/flynn/flynn/pkg/ctxhelper"
	"github.com/flynn/flynn/pkg/httphelper"
	"github.com/julienschmidt/httprouter"
	"golang.org/x/net/context"
)

type Repository interface {
	Add(thing interface{}) error
	Get(id string) (interface{}, error)
	List() (interface{}, error)
}

type Remover interface {
	Remove(string) error
}

func crud(r *httprouter.Router, resource string, example interface{}, repo Repository) {
	resourceType := reflect.TypeOf(example)
	prefix := "/" + resource

	r.POST(prefix, httphelper.WrapHandler(func(ctx context.Context, rw http.ResponseWriter, req *http.Request) {
		thing := reflect.New(resourceType).Interface()
		if err := httphelper.DecodeJSON(req, thing); err != nil {
			respondWithError(rw, err)
			return
		}

		if err := schema.Validate(thing); err != nil {
			respondWithError(rw, err)
			return
		}

		if err := repo.Add(thing); err != nil {
			respondWithError(rw, err)
			return
		}
		httphelper.JSON(rw, 200, thing)
	}))

	lookup := func(ctx context.Context) (interface{}, error) {
		params, _ := ctxhelper.ParamsFromContext(ctx)
		return repo.Get(params.ByName(resource + "_id"))
	}

	singletonPath := prefix + "/:" + resource + "_id"
	r.GET(singletonPath, httphelper.WrapHandler(func(ctx context.Context, rw http.ResponseWriter, _ *http.Request) {
		thing, err := lookup(ctx)
		if err != nil {
			respondWithError(rw, err)
			return
		}
		httphelper.JSON(rw, 200, thing)
	}))

	r.GET(prefix, httphelper.WrapHandler(func(ctx context.Context, rw http.ResponseWriter, _ *http.Request) {
		list, err := repo.List()
		if err != nil {
			respondWithError(rw, err)
			return
		}
		httphelper.JSON(rw, 200, list)
	}))

	if remover, ok := repo.(Remover); ok {
		r.DELETE(singletonPath, httphelper.WrapHandler(func(ctx context.Context, rw http.ResponseWriter, _ *http.Request) {
			_, err := lookup(ctx)
			if err != nil {
				respondWithError(rw, err)
				return
			}
			params, _ := ctxhelper.ParamsFromContext(ctx)
			if err = remover.Remove(params.ByName(resource + "_id")); err != nil {
				respondWithError(rw, err)
				return
			}
			rw.WriteHeader(200)
		}))
	}
}
