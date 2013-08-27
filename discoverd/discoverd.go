package main

import (
	"fmt"
	"discover"
)

func main() {
	server := discover.NewServer()
	ttl := discover.MissedHearbeatTTL
	fmt.Printf("hello from dockerd %s", ttl)
}