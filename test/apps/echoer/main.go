package main

import (
	"bufio"
	"log"
	"net"
	"os"

	"github.com/flynn/flynn/discoverd/client"
)

const service = "echo-service"

func main() {
	port := os.Getenv("PORT")
	addr := ":" + port

	if err := discoverd.Register(service, addr); err != nil {
		log.Fatal(err)
	}

	l, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatal(err)
	}
	defer l.Close()
	log.Println("Listening on", addr)

	for {
		conn, err := l.Accept()
		if err != nil {
			log.Fatal(err)
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
