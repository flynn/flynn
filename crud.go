package main

import (
	"encoding/json"
	"net/http"
	"reflect"

	"github.com/codegangsta/martini"
	"github.com/codegangsta/martini-contrib/render"
)

type Repository interface {
	Add(thing interface{}) error
	Get(id string) (interface{}, error)
	List() (interface{}, error)
}
type Remover interface {
	Remove(string) error
}

func crud(resource string, example interface{}, repo Repository, r martini.Router) interface{} {
	resourceType := reflect.TypeOf(example)
	resourcePtr := reflect.PtrTo(resourceType)
	prefix := "/" + resource

	r.Post(prefix, func(req *http.Request, r render.Render) {
		thing := reflect.New(resourceType).Interface()
		err := json.NewDecoder(req.Body).Decode(thing)
		if err != nil {
			// 400?
			return
		}

		err = repo.Add(thing)
		if err != nil {
			// 500
			// log error
			return
		}
		r.JSON(200, thing)
	})

	lookup := func(c martini.Context, params martini.Params, req *http.Request, w http.ResponseWriter) {
		thing, err := repo.Get(params[resource+"_id"])
		if err != nil {
			if err == ErrNotFound {
				w.WriteHeader(404)
				return
			}
			// TODO: 500/log error
		}
		c.Map(thing)
	}

	r.Get(prefix+"/:"+resource+"_id", lookup, func(c martini.Context, r render.Render) {
		r.JSON(200, c.Get(resourcePtr).Interface())
	})

	r.Get(prefix, func(r render.Render) {
		list, err := repo.List()
		if err != nil {
			// TODO: 500/log error
		}
		r.JSON(200, list)
	})

	if remover, ok := repo.(Remover); ok {
		r.Delete(prefix+"/:"+resource+"_id", lookup, func(c martini.Context, params martini.Params) {
			err := remover.Remove(params[resource+"_id"])
			if err != nil {
				// TODO: 500/log error
			}
		})
	}

	return lookup
}
