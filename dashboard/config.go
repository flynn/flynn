package main

import (
	"fmt"
	"log"
	"os"
	"path"
	"strings"

	ct "github.com/flynn/flynn/controller/types"
	"github.com/gorilla/sessions"
)

type Config struct {
	Addr                    string
	DefaultRouteDomain      string
	ControllerDomain        string
	ControllerKey           string
	StatusDomain            string
	StatusKey               string
	URL                     string
	InterfaceURL            string
	PathPrefix              string
	CookiePath              string
	SecureCookies           bool
	LoginToken              string
	GithubToken             string
	GithubAPIURL            string
	GithubTokenURL          string
	GithubCloneAuthRequired bool
	SessionStore            *sessions.CookieStore
	AppName                 string
	InstallCert             bool
	Cache                   bool
	DefaultDeployTimeout    int
}

func LoadConfigFromEnv() *Config {
	conf := &Config{}
	port := os.Getenv("PORT")
	if port == "" {
		port = "5000"
	}
	conf.Addr = ":" + port

	conf.DefaultRouteDomain = os.Getenv("DEFAULT_ROUTE_DOMAIN")
	if conf.DefaultRouteDomain == "" {
		log.Fatal("DEFAULT_ROUTE_DOMAIN is required!")
	}

	conf.ControllerDomain = os.Getenv("CONTROLLER_DOMAIN")
	if conf.ControllerDomain == "" {
		log.Fatal("CONTROLLER_DOMAIN is required!")
	}

	conf.ControllerKey = os.Getenv("CONTROLLER_KEY")
	if conf.ControllerKey == "" {
		log.Fatal("CONTROLLER_KEY is required!")
	}

	conf.StatusDomain = fmt.Sprintf("status.%s", conf.DefaultRouteDomain)
	conf.StatusKey = os.Getenv("STATUS_KEY")

	conf.URL = os.Getenv("URL")
	if conf.URL == "" {
		log.Fatal("URL is required!")
	}
	conf.InterfaceURL = conf.URL

	sessionSecret := os.Getenv("SESSION_SECRET")
	if sessionSecret == "" {
		log.Fatal("SESSION_SECRET is required!")
	}
	conf.SessionStore = sessions.NewCookieStore([]byte(sessionSecret))

	pathPrefix := path.Clean(os.Getenv("PATH_PREFIX"))
	conf.CookiePath = pathPrefix
	if pathPrefix == "/" || pathPrefix == "." {
		pathPrefix = ""
		conf.CookiePath = "/"
	}
	conf.PathPrefix = pathPrefix

	conf.SecureCookies = os.Getenv("SECURE_COOKIES") != ""

	conf.LoginToken = os.Getenv("LOGIN_TOKEN")
	if conf.LoginToken == "" {
		log.Fatal("LOGIN_TOKEN is required")
	}

	conf.GithubToken = os.Getenv("GITHUB_TOKEN")

	if host := os.Getenv("GITHUB_ENTERPRISE_HOST"); host != "" {
		conf.GithubAPIURL = fmt.Sprintf("https://%s/api/v3", host)
		conf.GithubTokenURL = fmt.Sprintf("https://%s/settings/tokens/new", host)
		conf.GithubCloneAuthRequired = true
	} else {
		conf.GithubAPIURL = "https://api.github.com"
		conf.GithubTokenURL = "https://github.com/settings/tokens/new"
	}

	conf.AppName = os.Getenv("APP_NAME")
	if conf.AppName == "" {
		conf.AppName = "dashboard"
	}

	conf.InstallCert = strings.HasPrefix(conf.URL, "https://")

	conf.Cache = os.Getenv("DISABLE_CACHE") == ""

	conf.DefaultDeployTimeout = ct.DefaultDeployTimeout

	return conf
}
