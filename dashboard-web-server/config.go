package main

import (
	"log"
	"os"
	"path"

	"github.com/gorilla/sessions"
)

type Config struct {
	Addr          string
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
	conf.Addr = ":" + port

	conf.ClusterDomain = os.Getenv("CLUSTER_DOMAIN")
	if conf.ClusterDomain == "" {
		log.Fatal("CLUSTER_DOMAIN is required!")
	}

	conf.ControllerKey = os.Getenv("CONTROLLER_KEY")
	if conf.ControllerKey == "" {
		log.Fatal("CONTROLLER_KEY is required!")
	}


	conf.InterfaceURL = os.Getenv("INTERFACE_URL")
	if conf.InterfaceURL == "" {
		log.Fatal("INTERFACE_URL is required!")
	}

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

	secureCookies := os.Getenv("SECURE_COOKIES")
	if secureCookies == "" {
		conf.SecureCookies = false
	} else {
		conf.SecureCookies = true
	}

	conf.LoginToken = os.Getenv("LOGIN_TOKEN")
	if conf.LoginToken == "" {
		log.Fatal("LOGIN_TOKEN is required")
	}

	conf.GithubToken = os.Getenv("GITHUB_TOKEN")

	return conf
}
