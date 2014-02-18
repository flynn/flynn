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

func crud(resource string, example interface{}, repo Repository, r martini.Router) interface{} {
	resourceType := reflect.TypeOf(example)
	resourcePtr := reflect.PtrTo(resourceType)

	r.Post("/"+resource, func(req *http.Request, r render.Render) {
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

	r.Get("/"+resource+"/:"+resource+"_id", lookup, func(c martini.Context, r render.Render, w http.ResponseWriter) {
		r.JSON(200, c.Get(resourcePtr).Interface())
	})

	r.Get("/"+resource, func(r render.Render) {
		list, err := repo.List()
		if err != nil {
			// TODO: 500/log error
		}
		r.JSON(200, list)
	})

	return lookup
}
