package main

import (
	"fmt"
	"log"
	"net"
	"net/rpc"
	"os"

	"github.com/titanous/fdrpc"
)

type Obj struct {
}

func (o *Obj) GetStdOut(a struct{}, b *fdrpc.FD) error {
	fmt.Println("GetStdOut")
	b.FD = 1
	return nil
}

func (o *Obj) GetStreams(a struct{}, b *[]fdrpc.FD) error {
	fmt.Println("GetStreams")
	*b = []fdrpc.FD{{1}, {2}}
	return nil
}

func main() {
	if err := rpc.Register(&Obj{}); err != nil {
		log.Fatal(err)
	}

	os.Remove("/tmp/test.socket")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Net: "unix", Name: "/tmp/test.socket"})
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
