package main

import (
	"bufio"
	"log"
	"net"
	"os"

	"github.com/flynn/flynn/pkg/shutdown"
)

func main() {
	defer shutdown.Exit()

	port := os.Getenv("PORT")
	addr := ":" + port

	l, err := net.Listen("tcp", addr)
	if err != nil {
		shutdown.Fatal(err)
	}
	defer l.Close()
	log.Println("Listening on", addr)

	for {
		conn, err := l.Accept()
		if err != nil {
			shutdown.Fatal(err)
		}
		go handle(conn)
	}
}

func handle(conn net.Conn) {
	b := bufio.NewReader(conn)
	for {
		line, err := b.ReadBytes('\n')
		if err != nil {
			break
		}
		conn.Write(line)
	}
}
