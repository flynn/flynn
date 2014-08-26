package main

import (
	"log"
	"net"
	"net/http"
	"strings"

	"github.com/go-martini/martini"
	"github.com/gorilla/sessions"
	"github.com/martini-contrib/render"
)

type RequestHelper interface {
	Error(err error)
	JSON(int, interface{})
	IsAuthenticated() bool
	SetAuthenticated(*http.Request, http.ResponseWriter)
	UnsetAuthenticated(*http.Request, http.ResponseWriter)
	WriteHeader(int)
}

func reqHelperMiddleware(c martini.Context, req *http.Request, w http.ResponseWriter, r render.Render, conf *Config) {
	reqh := &reqHelper{
		Render:         r,
		ResponseWriter: w,
		req:            req,
		conf:           conf,
	}

	ip := req.Header.Get("X-Real-Ip")
	if xff := req.Header.Get("X-Forwarded-For"); ip == "" && xff != "" {
		ips := strings.Split(xff, ",")
		if len(ips) > 0 {
			ip = strings.TrimSpace(ips[0])
		}
	}
	if ip == "" {
		ip, _, _ = net.SplitHostPort(req.RemoteAddr)
		if idx := strings.IndexByte(ip, '%'); idx > -1 {
			// Strip IPv6 scope id like 'fe80::1%lo0'
			ip = ip[:idx]
		}
	}

	session, _ := conf.SessionStore.Get(req, "session")
	reqh.session = session

	c.MapTo(reqh, (*RequestHelper)(nil))
}

type reqHelper struct {
	render.Render
	http.ResponseWriter

	session *sessions.Session
	req     *http.Request
	conf    *Config
}

func (rh *reqHelper) Error(err error) {
	switch err {
	case ErrNotFound:
		rh.WriteHeader(404)
		return
	case ErrInvalidLoginToken:
		rh.JSON(401, err)
		return
	}

	log.Println(err)

	rh.JSON(500, map[string]string{"message": "Internal server error"})
}

func (rh *reqHelper) IsAuthenticated() bool {
	return rh.session.Values["auth"] == true
}

func (rh *reqHelper) SetAuthenticated(req *http.Request, w http.ResponseWriter) {
	rh.session.Values["auth"] = true
	rh.session.Save(req, w)
}

func (rh *reqHelper) UnsetAuthenticated(req *http.Request, w http.ResponseWriter) {
	rh.session.Values["auth"] = false
	rh.session.Save(req, w)
}
