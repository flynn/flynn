package main

import (
	"encoding/json"
	"net/http"
	"reflect"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/julienschmidt/httprouter"
	"github.com/flynn/flynn/pkg/httphelper"
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

func crud(r *httprouter.Router, resource string, example interface{}, repo Repository) {
	resourceType := reflect.TypeOf(example)
	prefix := "/" + resource

	r.POST(prefix, func(rw http.ResponseWriter, req *http.Request, _ httprouter.Params) {
		thing := reflect.New(resourceType).Interface()
		err := json.NewDecoder(req.Body).Decode(thing)
		if err != nil {
			respondWithError(rw, err)
			return
		}

		err = repo.Add(thing)
		if err != nil {
			respondWithError(rw, err)
			return
		}
		httphelper.JSON(rw, 200, thing)
	})

	lookup := func(params httprouter.Params) (interface{}, error) {
		return repo.Get(params.ByName(resource + "_id"))
	}

	singletonPath := prefix + "/:" + resource + "_id"
	r.GET(singletonPath, func(rw http.ResponseWriter, _ *http.Request, params httprouter.Params) {
		thing, err := lookup(params)
		if err != nil {
			respondWithError(rw, err)
			return
		}
		httphelper.JSON(rw, 200, thing)
	})

	r.GET(prefix, func(rw http.ResponseWriter, _ *http.Request, _ httprouter.Params) {
		list, err := repo.List()
		if err != nil {
			respondWithError(rw, err)
			return
		}
		httphelper.JSON(rw, 200, list)
	})

	if remover, ok := repo.(Remover); ok {
		r.DELETE(singletonPath, func(rw http.ResponseWriter, _ *http.Request, params httprouter.Params) {
			_, err := lookup(params)
			if err != nil {
				respondWithError(rw, err)
				return
			}
			if err = remover.Remove(params.ByName(resource + "_id")); err != nil {
				respondWithError(rw, err)
				return
			}
			rw.WriteHeader(200)
		})
	}

	if updater, ok := repo.(Updater); ok {
		r.POST(singletonPath, func(rw http.ResponseWriter, req *http.Request, params httprouter.Params) {
			var data map[string]interface{}
			if err := json.NewDecoder(req.Body).Decode(&data); err != nil {
				respondWithError(rw, err)
				return
			}
			app, err := updater.Update(params.ByName(resource+"_id"), data)
			if err != nil {
				respondWithError(rw, err)
				return
			}
			httphelper.JSON(rw, 200, app)
		})
	}
}
