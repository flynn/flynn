package main

import (
	"encoding/json"
	"log"
	"net/http"
	"reflect"

	"github.com/go-martini/martini"
	"github.com/martini-contrib/render"
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

	r.Post(prefix, func(req *http.Request, r render.Render) {
		thing := reflect.New(resourceType).Interface()
		err := json.NewDecoder(req.Body).Decode(thing)
		if err != nil {
			// 400?
			return
		}

		err = repo.Add(thing)
		if err != nil {
			log.Println(err)
			r.JSON(500, struct{}{})
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
			log.Println(err)
			w.WriteHeader(500)
			return
		}
		c.Map(thing)
	}

	singletonPath := prefix + "/:" + resource + "_id"
	r.Get(singletonPath, lookup, func(c martini.Context, r render.Render) {
		r.JSON(200, c.Get(resourcePtr).Interface())
	})

	r.Get(prefix, func(r render.Render) {
		list, err := repo.List()
		if err != nil {
			log.Println(err)
			r.JSON(500, struct{}{})
			return
		}
		r.JSON(200, list)
	})

	if remover, ok := repo.(Remover); ok {
		r.Delete(singletonPath, lookup, func(params martini.Params, w http.ResponseWriter) {
			if err := remover.Remove(params[resource+"_id"]); err != nil {
				log.Println(err)
				w.WriteHeader(500)
				return
			}
		})
	}

	if updater, ok := repo.(Updater); ok {
		r.Post(singletonPath, func(params martini.Params, req *http.Request, r render.Render) {
			var data map[string]interface{}
			if err := json.NewDecoder(req.Body).Decode(&data); err != nil {
				r.JSON(400, struct{}{})
				return
			}
			app, err := updater.Update(params[resource+"_id"], data)
			if err != nil {
				log.Println(err)
				r.JSON(500, struct{}{})
				return
			}
			r.JSON(200, app)
		})
	}

	return lookup
}
