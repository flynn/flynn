package main

import (
	"fdrpc"
	"fmt"
	"log"
	"net"
	"net/rpc"
	"os"
)

type RpcObject struct {
}

func (o *RpcObject) GetStdOut(a int, b *fdrpc.RpcFD) error {
	fmt.Printf("GetStdOut %d\n", a)
	b.Fd = 1
	return nil
}

func main() {
	object := &RpcObject{}

	if err := rpc.Register(object); err != nil {
		log.Fatal(err)
	}

	os.Remove("/tmp/test.socket")
	addr := &net.UnixAddr{Net: "unix", Name: "/tmp/test.socket"}
	listener, err := net.ListenUnix("unix", addr)
	if err != nil {
		log.Fatal(err)
	}

	for {
		conn, err := listener.AcceptUnix()
		if err != nil {
			log.Printf("rpc socket accept error: %s", err)
			continue
		}

		fmt.Printf("New client connected\n")
		fdrpc.ServeConn(conn)
		conn.Close()
	}
}
