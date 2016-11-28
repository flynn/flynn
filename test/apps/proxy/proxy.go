package main

import (
	"io"
	"log"
	"net"
	"os"
	"sync"

	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/dialer"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	l, err := net.Listen("tcp", os.Getenv("EXTERNAL_IP")+":0")
	if err != nil {
		return err
	}
	defer l.Close()

	hb, err := discoverd.AddServiceAndRegister(os.Getenv("SERVICE"), l.Addr().String())
	if err != nil {
		return err
	}
	defer hb.Close()

	for {
		conn, err := l.Accept()
		if err != nil {
			return err
		}
		go proxy(conn)
	}
}

func proxy(client net.Conn) {
	defer client.Close()

	conn, err := dialer.Retry.Dial("tcp", os.Getenv("ADDR"))
	if err != nil {
		return
	}
	defer conn.Close()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		io.Copy(conn, client)
		conn.(*net.TCPConn).CloseWrite()
	}()
	go func() {
		defer wg.Done()
		io.Copy(client, conn)
		client.(*net.TCPConn).CloseWrite()
	}()
	wg.Wait()
}
