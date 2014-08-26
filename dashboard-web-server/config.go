package main

import (
	"log"
	"os"
	"path"
	"strings"

	"github.com/dchest/uniuri"
	"github.com/gorilla/sessions"
)

type Config struct {
	Port          string
	ClusterDomain string
	ControllerKey string
	InterfaceURL  string
	PathPrefix    string
	CookiePath    string
	SecureCookies bool
	LoginToken    string
	GithubToken   string
	SessionStore  *sessions.CookieStore
}

func LoadConfigFromEnv() *Config {
	conf := &Config{}
	port := os.Getenv("PORT")
	if port == "" {
		port = "5000"
	}
	port = strings.Join([]string{":", port}, "")
	conf.Port = port

	clusterDomain := os.Getenv("CLUSTER_DOMAIN")
	if clusterDomain == "" {
		log.Fatal("CLUSTER_DOMAIN is required!")
	}
	conf.ClusterDomain = clusterDomain

	controllerKey := os.Getenv("CONTROLLER_KEY")
	if controllerKey == "" {
		log.Fatal("CONTROLLER_KEY is required!")
	}
	conf.ControllerKey = controllerKey

	interfaceURL := os.Getenv("INTERFACE_URL")
	if interfaceURL == "" {
		log.Fatal("INTERFACE_URL is required!")
	}
	conf.InterfaceURL = interfaceURL

	sessionSecret := os.Getenv("SESSION_SECRET")
	if sessionSecret == "" {
		sessionSecret = uniuri.NewLen(64)
	}
	conf.SessionStore = sessions.NewCookieStore([]byte(sessionSecret))

	pathPrefix := path.Clean(os.Getenv("PATH_PREFIX"))
	conf.CookiePath = pathPrefix
	if pathPrefix == "/" || pathPrefix == "." {
		pathPrefix = ""
		conf.CookiePath = "/"
	}
	conf.PathPrefix = pathPrefix

	secureCookies := os.Getenv("SECURE_COOKIES")
	if secureCookies == "" {
		conf.SecureCookies = false
	} else {
		conf.SecureCookies = true
	}

	conf.LoginToken = os.Getenv("LOGIN_TOKEN")
	if conf.LoginToken == "" {
		log.Fatal("LOGIN_TOKEN is required")
	} else if len(conf.LoginToken) < 6 {
		log.Fatal("LOGIN_TOKEN must be at least six characters long")
	}

	conf.GithubToken = os.Getenv("GITHUB_TOKEN")

	return conf
}
