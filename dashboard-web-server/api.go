package main

import (
	"fmt"
	"io"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/codegangsta/inject"
	"github.com/go-martini/martini"
	"github.com/martini-contrib/binding"
	"github.com/martini-contrib/render"
	"github.com/titanous/cors"
)

type LoginInfo struct {
	Token string `json:"token"`
}

func APIHandler(conf *Config) http.Handler {
	r := martini.NewRouter()
	m := martini.New()
	m.Use(martini.Logger())
	m.Use(apiPanicHandler())
	m.Use(render.Renderer())
	m.Action(r.Handle)
	m.SetParent(configInjector(conf))

	m.Use(cors.Allow(&cors.Options{
		AllowOrigins:     []string{conf.InterfaceURL},
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD"},
		AllowHeaders:     []string{"Authorization", "Accept", "Content-Type", "If-Match", "If-None-Match"},
		ExposeHeaders:    []string{"ETag"},
		AllowCredentials: true,
		MaxAge:           time.Hour,
	}))

	r.Group(conf.PathPrefix, func(r martini.Router) {
		m.Use(reqHelperMiddleware)
		r.Post("/user/sessions", binding.Json(LoginInfo{}), login)
		r.Delete("/user/session", logout)

		r.Group("", func(r martini.Router) {
			r.Any("/flynn.*", flynnProxy)
		}, requireUserMiddleware)

		r.Get("/config", getConfig)

		r.Get("/", http.RedirectHandler(conf.InterfaceURL, http.StatusMovedPermanently).ServeHTTP)
	})

	return m
}

func apiPanicHandler() martini.Handler {
	return func(c martini.Context, req *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				wval := c.Get(inject.InterfaceOf((*http.ResponseWriter)(nil)))
				w := wval.Interface().(http.ResponseWriter)

				fmt.Println(err)
				debug.PrintStack()
				w.WriteHeader(500)
			}
		}()
		c.Next()
	}
}

func configInjector(conf *Config) inject.Injector {
	m := inject.New()
	m.Map(conf)
	return m
}

func requireUserMiddleware(rh RequestHelper) {
	if !rh.IsAuthenticated() {
		rh.WriteHeader(401)
	}
}

func login(req *http.Request, w http.ResponseWriter, info LoginInfo, rh RequestHelper, conf *Config) {
	if info.Token == conf.LoginToken {
		rh.SetAuthenticated(req, w)
	} else {
		rh.Error(ErrInvalidLoginToken)
		return
	}
	rh.WriteHeader(200)
}

func logout(req *http.Request, w http.ResponseWriter, rh RequestHelper) {
	rh.UnsetAuthenticated(req, w)
	rh.WriteHeader(200)
}

func flynnProxy(req *http.Request, w http.ResponseWriter, params martini.Params, conf *Config, rh RequestHelper) {
	client := &http.Client{}
	path := strings.TrimPrefix(req.RequestURI, "/flynn")
	domain := conf.ClusterDomain
	newReq, err := http.NewRequest(req.Method, fmt.Sprintf("http://%s%s", domain, path), req.Body)
	if err != nil {
		fmt.Errorf("%v", err)
		return
	}
	for _, k := range []string{"Content-Type", "Accept", "Content-Length"} {
		if v, ok := req.Header[k]; ok {
			newReq.Header[k] = v
		}
	}
	newReq.SetBasicAuth("", conf.ControllerKey)
	newReq.ContentLength = req.ContentLength
	res, err := client.Do(newReq)
	if err != nil {
		rh.JSON(503, &ServerError{Message: err.Error()})
		return
	}
	for k, v := range res.Header {
		w.Header()[k] = v
	}
	if strings.Contains(res.Header.Get("Content-Type"), "text/event-stream") {
		w.Header().Set("X-Accel-Buffering", "no")
	}
	w.WriteHeader(res.StatusCode)

	go func() {
		<-w.(http.CloseNotifier).CloseNotify()
		res.Body.Close()
	}()

	io.Copy(flushWriter{w}, res.Body)
	res.Body.Close()
}

type flushWriter struct {
	w io.Writer
}

func (f flushWriter) Write(p []byte) (int, error) {
	defer func() {
		if fw, ok := f.w.(http.Flusher); ok {
			fw.Flush()
		}
	}()
	return f.w.Write(p)
}

type OAuthToken struct {
	AccessToken string `json:"access_token"`
}

type ExpandedUser struct {
	Auths map[string]*OAuthToken `json:"auths"`
}

type UserConfig struct {
	User *ExpandedUser `json:"user,omitempty"`

	Endpoints map[string]string `json:"endpoints"`
}

var baseConfig = UserConfig{
	Endpoints: map[string]string{
		"login":              "/user/sessions",
		"logout":             "/user/session",
		"cluster_controller": "/flynn",
	},
}

func getConfig(rh RequestHelper, conf *Config) {
	config := baseConfig

	fmt.Printf("IsAuthenticated: %v\n", rh.IsAuthenticated())
	if rh.IsAuthenticated() {
		config.User = &ExpandedUser{}
		config.User.Auths = make(map[string]*OAuthToken)

		if conf.GithubToken != "" {
			config.User.Auths["github"] = &OAuthToken{AccessToken: conf.GithubToken}
		}
	}

	rh.JSON(200, config)
}
