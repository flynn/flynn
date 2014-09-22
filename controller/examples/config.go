package main

import (
	"fmt"
	"net"
	"os"
	"strings"
)

type config struct {
	controllerDomain string
	controllerKey    string
	ourAddr          string
	ourPort          string
}

func loadConfigFromEnv() (*config, error) {
	c := &config{}
	c.controllerDomain = os.Getenv("CONTROLLER_DOMAIN")
	if c.controllerDomain == "" {
		return nil, fmt.Errorf("CONTROLLER_DOMAIN is required")
	}
	c.controllerKey = os.Getenv("CONTROLLER_KEY")
	if c.controllerKey == "" {
		return nil, fmt.Errorf("CONTROLLER_KEY is required")
	}
	c.ourAddr = os.Getenv("ADDR")
	if c.ourAddr == "" {
		err := c.discoverAddr()
		if err != nil {
			return nil, fmt.Errorf("Discovery failed, ADDR is required")
		}
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "4456"
	}
	c.ourPort = port
	return c, nil
}

func (c *config) discoverAddr() error {
	controllerAddrs, err := net.LookupHost(c.controllerDomain)
	if err != nil {
		return err
	}
	ints, err := net.Interfaces()
	if err != nil {
		return err
	}
	addrs := make([]net.IP, 0, len(ints))
	for _, i := range ints {
		iAddrs, err := i.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range iAddrs {
			ip := net.ParseIP(strings.Split(addr.String(), "/")[0])
			if ip == nil {
				continue
			}
			addrs = append(addrs, ip)
		}
	}
	var ourAddr string
	for _, cAddr := range controllerAddrs {
		cIP := net.ParseIP(strings.Split(cAddr, "/")[0])
		if cIP == nil || len(cIP) != 16 {
			continue
		}
		for _, iAddr := range addrs {
			if len(iAddr) != 16 || cIP[12] != iAddr[12] || cIP[13] != iAddr[13] || cIP[14] != iAddr[14] {
				continue
			}
			ourAddr = iAddr.String()
			break
		}
	}
	if ourAddr == "" {
		return fmt.Errorf("No interface found")
	}
	ourAddr = strings.Split(ourAddr, "/")[0]
	c.ourAddr = ourAddr
	return nil
}
