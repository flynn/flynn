package main

import (
	"encoding/json"
	"net/http"
	"reflect"

	"github.com/go-martini/martini"
)

type Repository interface {
	Add(thing interface{}) error
	Get(id string) (interface{}, error)
	List() (interface{}, error)
}

type Remover interface {
	Remove(string) error
}

type Updater interface {
	Update(string, map[string]interface{}) (interface{}, error)
}

func crud(resource string, example interface{}, repo Repository, r martini.Router) interface{} {
	resourceType := reflect.TypeOf(example)
	resourcePtr := reflect.PtrTo(resourceType)
	prefix := "/" + resource

	r.Post(prefix, func(req *http.Request, r ResponseHelper) {
		thing := reflect.New(resourceType).Interface()
		err := json.NewDecoder(req.Body).Decode(thing)
		if err != nil {
			r.Error(err)
			return
		}

		err = repo.Add(thing)
		if err != nil {
			r.Error(err)
			return
		}
		r.JSON(200, thing)
	})

	lookup := func(c martini.Context, params martini.Params, req *http.Request, r ResponseHelper) {
		thing, err := repo.Get(params[resource+"_id"])
		if err != nil {
			r.Error(err)
			return
		}
		c.Map(thing)
	}

	singletonPath := prefix + "/:" + resource + "_id"
	r.Get(singletonPath, lookup, func(c martini.Context, r ResponseHelper) {
		r.JSON(200, c.Get(resourcePtr).Interface())
	})

	r.Get(prefix, func(r ResponseHelper) {
		list, err := repo.List()
		if err != nil {
			r.Error(err)
			return
		}
		r.JSON(200, list)
	})

	if remover, ok := repo.(Remover); ok {
		r.Delete(singletonPath, lookup, func(params martini.Params, r ResponseHelper) {
			if err := remover.Remove(params[resource+"_id"]); err != nil {
				r.Error(err)
				return
			}
		})
	}

	if updater, ok := repo.(Updater); ok {
		r.Post(singletonPath, func(params martini.Params, req *http.Request, r ResponseHelper) {
			var data map[string]interface{}
			if err := json.NewDecoder(req.Body).Decode(&data); err != nil {
				r.Error(err)
				return
			}
			app, err := updater.Update(params[resource+"_id"], data)
			if err != nil {
				r.Error(err)
				return
			}
			r.JSON(200, app)
		})
	}

	return lookup
}
