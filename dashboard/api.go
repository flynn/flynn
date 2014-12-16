package main

import (
	"crypto/subtle"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/badgerodon/ioutil"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/go-martini/martini"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/martini-contrib/binding"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/martini-contrib/render"
	"github.com/flynn/flynn/pkg/cors"
)

type LoginInfo struct {
	Token string `json:"token"`
}

func APIHandler(conf *Config) http.Handler {
	r := martini.NewRouter()
	m := martini.New()
	m.Use(martini.Logger())
	m.Use(martini.Recovery())
	m.Use(render.Renderer(render.Options{
		Directory:  conf.StaticPath,
		Extensions: []string{".html"},
	}))
	m.Action(r.Handle)

	m.Map(conf)

	httpInterfaceURL := conf.InterfaceURL
	if strings.HasPrefix(conf.InterfaceURL, "https") {
		httpInterfaceURL = "http" + strings.TrimPrefix(conf.InterfaceURL, "https")
	}

	m.Use(cors.Allow(&cors.Options{
		AllowOrigins:     []string{conf.InterfaceURL, httpInterfaceURL},
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

		r.Get("/config", getConfig)
		r.Get("/cert", getCert)

		r.Any("/assets/dashboard.*.js", serveDashboardJs)

		r.Any("/assets.*", martini.Static(filepath.Join(conf.StaticPath, "assets"), martini.StaticOptions{
			Prefix: "/assets",
		}))

		r.Get("/.*", func(r render.Render) {
			r.HTML(200, "dashboard", "")
		})
	})

	return m
}

func requireUserMiddleware(rh RequestHelper) {
	if !rh.IsAuthenticated() {
		rh.WriteHeader(401)
	}
}

func login(req *http.Request, w http.ResponseWriter, info LoginInfo, rh RequestHelper, conf *Config) {
	if len(info.Token) != len(conf.LoginToken) || subtle.ConstantTimeCompare([]byte(info.Token), []byte(conf.LoginToken)) != 1 {
		rh.Error(ErrInvalidLoginToken)
		return
	}
	rh.SetAuthenticated()
	rh.WriteHeader(200)
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

func serveDashboardJs(res http.ResponseWriter, req *http.Request, conf *Config) {
	file := filepath.Join(conf.StaticPath, "assets", filepath.Base(req.URL.Path))
	f, err := os.Open(file)
	if err != nil {
		fmt.Println(err)
		res.WriteHeader(500)
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		fmt.Println(err)
		return
	}

	jsConf := strings.NewReader(fmt.Sprintf(`
    window.DashboardConfig = {
      APP_NAME: "%s",
      API_SERVER: "%s",
      PATH_PREFIX: "%s",
      INSTALL_CERT: %t
    };
  `, conf.AppName, conf.URL, conf.PathPrefix, len(conf.CACert) > 0))

	r := ioutil.NewMultiReadSeeker(jsConf, f)

	http.ServeContent(res, req, file, fi.ModTime(), r)
}
