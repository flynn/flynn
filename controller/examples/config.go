package main

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
)

type config struct {
	controllerKey string
	ourPort       string
	logOut        io.Writer
}

func loadConfigFromEnv() (*config, error) {
	c := &config{}
	c.controllerKey = os.Getenv("CONTROLLER_KEY")
	if c.controllerKey == "" {
		return nil, fmt.Errorf("CONTROLLER_KEY is required")
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "4456"
	}
	c.ourPort = port

	logPath := os.Getenv("LOGFILE")
	c.logOut = ioutil.Discard
	if logPath != "" {
		if f, err := os.OpenFile(logPath, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666); err == nil {
			c.logOut = f
		}
	}
	return c, nil
}
