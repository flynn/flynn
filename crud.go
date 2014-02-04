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
}

func crud(resource string, example interface{}, repo Repository, r martini.Router) {
	resourceType := reflect.TypeOf(example)

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

	r.Get("/"+resource+"/:id", func(params martini.Params, r render.Render, w http.ResponseWriter) {
		thing, err := repo.Get(params["id"])
		if err != nil {
			if err == ErrNotFound {
				w.WriteHeader(404)
				return
			}
			// TODO: 500/log error
		}
		r.JSON(200, thing)
	})
}
