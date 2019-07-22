package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/gorilla/sessions"
)

type Config struct {
	Addr              string
	ControllerDomain  string
	ControllerAuthKey string
	InterfaceURL      string
	LoginToken        string
	SessionStore      *sessions.CookieStore
	PublicConfig      map[string]string
	PublicConfigJSON  []byte
	PrivateConfig     map[string]string
	PrivateConfigJSON []byte
}

func MustConfig() *Config {
	conf := &Config{}
	port := os.Getenv("PORT")
	if port == "" {
		port = "5200"
	}
	conf.Addr = ":" + port

	conf.ControllerDomain = os.Getenv("CONTROLLER_DOMAIN")
	if conf.ControllerDomain == "" {
		log.Fatal("CONTROLLER_DOMAIN is required!")
	}

	conf.ControllerAuthKey = os.Getenv("CONTROLLER_AUTH_KEY")
	if conf.ControllerAuthKey == "" {
		log.Fatal("CONTROLLER_AUTH_KEY is required!")
	}

	conf.LoginToken = os.Getenv("LOGIN_TOKEN")
	if conf.LoginToken == "" {
		log.Fatal("LOGIN_TOKEN is required!")
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
	conf.SessionStore.Options.Secure = true
	if d := os.Getenv("SESSION_DOMAIN"); d != "" {
		conf.SessionStore.Options.Domain = d
	} else {
		log.Fatal("SESSION_DOMAIN is required!")
	}

	conf.PublicConfig = map[string]string{
		"CONTROLLER_HOST": fmt.Sprintf("https://%s", conf.ControllerDomain),
		"PUBLIC_URL":      conf.InterfaceURL,
	}

	var err error
	conf.PublicConfigJSON, err = json.Marshal(conf.PublicConfig)
	if err != nil {
		log.Fatalf("Error encoding PublicConfigJSON: %v", err)
	}

	conf.PrivateConfig = map[string]string{
		"CONTROLLER_AUTH_KEY": conf.ControllerAuthKey,
	}

	conf.PrivateConfigJSON, err = json.Marshal(conf.PrivateConfig)
	if err != nil {
		log.Fatalf("Error encoding PrivateConfigJSON: %v", err)
	}

	return conf
}
