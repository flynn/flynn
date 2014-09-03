package main

import (
	"log"
	"net/http"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/go-martini/martini"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/gorilla/sessions"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/martini-contrib/render"
)

type RequestHelper interface {
	Error(err error)
	JSON(int, interface{})
	IsAuthenticated() bool
	SetAuthenticated()
	UnsetAuthenticated()
	WriteHeader(int)
}

func reqHelperMiddleware(c martini.Context, req *http.Request, w http.ResponseWriter, r render.Render, conf *Config) {
	rh := &reqHelper{
		Render:         r,
		ResponseWriter: w,
		req:            req,
		conf:           conf,
	}

	rh.session, _ = conf.SessionStore.Get(req, "session")

	c.MapTo(rh, (*RequestHelper)(nil))
}

type reqHelper struct {
	render.Render
	http.ResponseWriter

	session *sessions.Session
	req     *http.Request
	conf    *Config
}

func (rh *reqHelper) Error(err error) {
	if err == ErrInvalidLoginToken {
		rh.JSON(401, err)
		return
	}

	log.Println(err)

	rh.JSON(500, map[string]string{"message": "Internal server error"})
}

func (rh *reqHelper) IsAuthenticated() bool {
	return rh.session.Values["auth"] == true
}

func (rh *reqHelper) SetAuthenticated() {
	rh.session.Values["auth"] = true
	rh.session.Save(rh.req, rh.ResponseWriter)
}

func (rh *reqHelper) UnsetAuthenticated() {
	delete(rh.session.Values, "auth")
	rh.session.Save(rh.req, rh.ResponseWriter)
}
