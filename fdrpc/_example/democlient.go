package main

import (
	"log"
	"syscall"

	"github.com/titanous/fdrpc"
)

func main() {
	log.SetFlags(log.Lshortfile)

	c, err := fdrpc.Dial("/tmp/test.socket")
	if err != nil {
		log.Fatal(err)
	}

	var fd fdrpc.FD
	if err := c.Call("Obj.GetStdOut", struct{}{}, &fd); err != nil {
		log.Fatal(err)
	}
	syscall.Write(fd.FD, []byte("Hello from request 1\n"))

	if err := c.Call("Obj.GetStdOut", struct{}{}, &fd); err != nil {
		log.Fatal(err)
	}
	syscall.Write(fd.FD, []byte("Hello from request 2\n"))

	var streams []fdrpc.FD
	if err := c.Call("Obj.GetStreams", struct{}{}, &streams); err != nil {
		log.Fatal(err)
	}
	syscall.Write(streams[0].FD, []byte("Hello stdout\n"))
	syscall.Write(streams[1].FD, []byte("Hello stderr\n"))
}
