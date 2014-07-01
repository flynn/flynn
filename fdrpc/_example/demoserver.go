package main

import (
	"fmt"
	"log"
	"os"

	"github.com/flynn/rpcplus"
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
	if err := rpcplus.Register(&Obj{}); err != nil {
		log.Fatal(err)
	}

	os.Remove("/tmp/test.socket")
	log.Fatal(fdrpc.ListenAndServe("/tmp/test.socket"))
}
