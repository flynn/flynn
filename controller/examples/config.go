package main

import (
	"fmt"
	"io"
	"log"
	"os"
)

type config struct {
	controllerKey string
	ourPort       string
	logOut        io.Writer
}

func init() {
	log.SetFlags(log.Lshortfile | log.Lmicroseconds)
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

	if logPath := os.Getenv("LOGFILE"); logPath != "" {
		if f, err := os.OpenFile(logPath, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666); err == nil {
			c.logOut = f
		}
	}
	if c.logOut == nil {
		c.logOut = os.Stderr
	}
	return c, nil
}
