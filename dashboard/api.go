package main

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/badgerodon/ioutil"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/go-martini/martini"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/martini-contrib/binding"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/martini-contrib/render"
	"github.com/flynn/flynn/pkg/cors"
	"github.com/flynn/flynn/pkg/status"
)

type LoginInfo struct {
	Token string `json:"token" form:"token"`
}

func AssetReader(path string) (io.ReadSeeker, time.Time, error) {
	t := time.Time{}
	data, err := Asset(path)
	if err != nil {
		return nil, t, err
	}
	if fi, err := AssetInfo(path); err != nil {
		t = fi.ModTime()
	}
	return bytes.NewReader(data), t, nil
}

func APIHandler(conf *Config) http.Handler {
	r := martini.NewRouter()
	m := martini.New()
	m.Use(martini.Logger())
	m.Use(martini.Recovery())
	m.Use(render.Renderer(render.Options{}))
	m.Action(r.Handle)

	m.Map(conf)

	httpInterfaceURL := conf.InterfaceURL
	if strings.HasPrefix(conf.InterfaceURL, "https") {
		httpInterfaceURL = "http" + strings.TrimPrefix(conf.InterfaceURL, "https")
	}

	m.Use(corsHandler(&cors.Options{
		AllowOrigins:     []string{conf.InterfaceURL, httpInterfaceURL},
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD"},
		AllowHeaders:     []string{"Authorization", "Accept", "Content-Type", "If-Match", "If-None-Match"},
		ExposeHeaders:    []string{"ETag"},
		AllowCredentials: true,
		MaxAge:           time.Hour,
	}))

	r.Get(status.Path, status.HealthyHandler)

	r.Group(conf.PathPrefix, func(r martini.Router) {
		m.Use(reqHelperMiddleware)
		r.Post("/user/sessions", binding.Bind(LoginInfo{}), login)
		r.Delete("/user/session", logout)

		r.Get("/config", getConfig)
		r.Get("/cert", getCert)

		r.Any("/assets/dashboard.*.js", serveDashboardJs)

		r.Any("/assets.*", serveAsset)

		r.Get("/ping", pingHandler)

		r.Get("/.*", serveTemplate)
	})

	return m
}

func corsHandler(corsOptions *cors.Options) http.HandlerFunc {
	defaultCorsHandler := cors.Allow(corsOptions)
	return func(w http.ResponseWriter, req *http.Request) {
		origin := req.Header.Get("Origin")
		if req.URL.Path == "/ping" && strings.HasPrefix(origin, "http://localhost:") {
			cors.Allow(&cors.Options{
				AllowOrigins:     []string{origin},
				AllowMethods:     []string{"GET"},
				AllowHeaders:     corsOptions.AllowHeaders,
				ExposeHeaders:    corsOptions.ExposeHeaders,
				AllowCredentials: corsOptions.AllowCredentials,
				MaxAge:           corsOptions.MaxAge,
			})(w, req)
		} else {
			defaultCorsHandler(w, req)
		}
	}
}

func requireUserMiddleware(rh RequestHelper) {
	if !rh.IsAuthenticated() {
		rh.WriteHeader(401)
	}
}

func pingHandler(req *http.Request, w http.ResponseWriter, rh RequestHelper) {
	rh.WriteHeader(200)
}

func serveStatic(w http.ResponseWriter, req *http.Request, path string) {
	data, t, err := AssetReader(path)
	if err != nil {
		fmt.Println(err)
		w.WriteHeader(404)
		return
	}

	ext := filepath.Ext(path)
	if mimeType := mime.TypeByExtension(ext); mimeType != "" {
		w.Header().Add("Content-Type", mimeType)
	}
	if ext == ".html" {
		w.Header().Add("Cache-Control", "max-age=0")
	}

	http.ServeContent(w, req, path, t, data)
}

func serveTemplate(w http.ResponseWriter, req *http.Request, rh RequestHelper, r render.Render, conf *Config) {
	serveStatic(w, req, filepath.Join("app", "build", "dashboard.html"))
}

func serveAsset(w http.ResponseWriter, req *http.Request, rh RequestHelper, conf *Config) {
	serveStatic(w, req, filepath.Join("app", "build", req.URL.Path))
}

func login(req *http.Request, w http.ResponseWriter, info LoginInfo, rh RequestHelper, conf *Config) {
	if len(info.Token) != len(conf.LoginToken) || subtle.ConstantTimeCompare([]byte(info.Token), []byte(conf.LoginToken)) != 1 {
		rh.Error(ErrInvalidLoginToken)
		return
	}
	rh.SetAuthenticated()
	if strings.Contains(req.Header.Get("Content-Type"), "form-urlencoded") {
		http.Redirect(w, req, conf.CookiePath, 302)
	} else {
		rh.WriteHeader(200)
	}
}

func logout(req *http.Request, w http.ResponseWriter, rh RequestHelper) {
	rh.UnsetAuthenticated()
	rh.WriteHeader(200)
}

type OAuthToken struct {
	AccessToken string `json:"access_token"`
}

type ExpandedUser struct {
	Auths         map[string]*OAuthToken `json:"auths"`
	ControllerKey string                 `json:"controller_key"`
}

type UserConfig struct {
	User *ExpandedUser `json:"user,omitempty"`

	Endpoints          map[string]string `json:"endpoints"`
	DefaultRouteDomain string            `json:"default_route_domain"`
}

var baseConfig = UserConfig{
	Endpoints: map[string]string{
		"login":  "/user/sessions",
		"logout": "/user/session",
	},
}

func getConfig(rh RequestHelper, conf *Config) {
	config := baseConfig

	config.Endpoints["cluster_controller"] = fmt.Sprintf("https://%s", conf.ControllerDomain)
	config.DefaultRouteDomain = conf.DefaultRouteDomain

	if rh.IsAuthenticated() {
		config.User = &ExpandedUser{}
		config.User.Auths = make(map[string]*OAuthToken)

		if conf.GithubToken != "" {
			config.User.Auths["github"] = &OAuthToken{AccessToken: conf.GithubToken}
		}

		config.User.ControllerKey = conf.ControllerKey
	}

	rh.JSON(200, config)
}

func getCert(w http.ResponseWriter, conf *Config) {
	w.Header().Set("Content-Type", "application/x-x509-ca-cert")
	w.Write(conf.CACert)
}

type DashboardConfig struct {
	AppName     string `json:"APP_NAME"`
	ApiServer   string `json:"API_SERVER"`
	PathPrefix  string `json:"PATH_PREFIX"`
	InstallCert bool   `json:"INSTALL_CERT"`
}

func serveDashboardJs(res http.ResponseWriter, req *http.Request, conf *Config) {
	path := filepath.Join("app", "build", "assets", filepath.Base(req.URL.Path))
	data, t, err := AssetReader(path)
	if err != nil {
		fmt.Println(err)
		res.WriteHeader(500)
		return
	}

	var jsConf bytes.Buffer
	jsConf.Write([]byte("window.DashboardConfig = "))
	json.NewEncoder(&jsConf).Encode(DashboardConfig{
		AppName:     conf.AppName,
		ApiServer:   conf.URL,
		PathPrefix:  conf.PathPrefix,
		InstallCert: len(conf.CACert) > 0,
	})
	jsConf.Write([]byte(";\n"))

	r := ioutil.NewMultiReadSeeker(bytes.NewReader(jsConf.Bytes()), data)

	http.ServeContent(res, req, path, t, r)
}
