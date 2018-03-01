// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rpcplus

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var (
	newServer                 *Server
	serverAddr, newServerAddr string
	httpServerAddr            string
	once, newOnce, httpOnce   sync.Once
)

const (
	newHttpPath = "/foo"
)

type Args struct {
	A, B int
}

type Reply struct {
	C int
}

type Arith int

// Some of Arith's methods have value args, some have pointer args. That's deliberate.

func (t *Arith) Add(args Args, reply *Reply) error {
	reply.C = args.A + args.B
	return nil
}

func (t *Arith) Mul(args *Args, reply *Reply) error {
	reply.C = args.A * args.B
	return nil
}

func (t *Arith) Div(args Args, reply *Reply) error {
	if args.B == 0 {
		return errors.New("divide by zero")
	}
	reply.C = args.A / args.B
	return nil
}

func (t *Arith) String(args *Args, reply *string) error {
	*reply = fmt.Sprintf("%d+%d=%d", args.A, args.B, args.A+args.B)
	return nil
}

func (t *Arith) Scan(args string, reply *Reply) (err error) {
	_, err = fmt.Sscan(args, &reply.C)
	return
}

func (t *Arith) Error(args *Args, reply *Reply) error {
	panic("ERROR")
}

func (t *Arith) TakesContext(context *string, args string, reply *string) error {
	return nil
}

func listenTCP() (net.Listener, string) {
	l, e := net.Listen("tcp", "127.0.0.1:0") // any available address
	if e != nil {
		log.Fatalf("net.Listen tcp :0: %v", e)
	}
	return l, l.Addr().String()
}

func startServer() {
	Register(new(Arith))

	var l net.Listener
	l, serverAddr = listenTCP()
	log.Println("Test RPC server listening on", serverAddr)
	go Accept(l)

	HandleHTTP()
	httpOnce.Do(startHttpServer)
}

func startNewServer() {
	newServer = NewServer()
	newServer.Register(new(Arith))

	var l net.Listener
	l, newServerAddr = listenTCP()
	log.Println("NewServer test RPC server listening on", newServerAddr)
	go Accept(l)

	newServer.HandleHTTP(newHttpPath, "/bar")
	httpOnce.Do(startHttpServer)
}

func startHttpServer() {
	server := httptest.NewServer(nil)
	httpServerAddr = server.Listener.Addr().String()
	log.Println("Test HTTP RPC server listening on", httpServerAddr)
}

func TestRPC(t *testing.T) {
	once.Do(startServer)
	testRPC(t, serverAddr)
	newOnce.Do(startNewServer)
	testRPC(t, newServerAddr)
}

func testRPC(t *testing.T, addr string) {
	client, err := Dial("tcp", addr)
	if err != nil {
		t.Fatal("dialing", err)
	}

	// Synchronous calls
	args := &Args{7, 8}
	reply := new(Reply)
	err = client.Call("Arith.Add", args, reply)
	if err != nil {
		t.Errorf("Add: expected no error but got string %q", err.Error())
	}
	if reply.C != args.A+args.B {
		t.Errorf("Add: expected %d got %d", reply.C, args.A+args.B)
	}

	// Nonexistent method
	args = &Args{7, 0}
	reply = new(Reply)
	err = client.Call("Arith.BadOperation", args, reply)
	// expect an error
	if err == nil {
		t.Error("BadOperation: expected error")
	} else if !strings.HasPrefix(err.Error(), "rpc: can't find method ") {
		t.Errorf("BadOperation: expected can't find method error; got %q", err)
	}

	// Unknown service
	args = &Args{7, 8}
	reply = new(Reply)
	err = client.Call("Arith.Unknown", args, reply)
	if err == nil {
		t.Error("expected error calling unknown service")
	} else if strings.Index(err.Error(), "method") < 0 {
		t.Error("expected error about method; got", err)
	}

	// Out of order.
	args = &Args{7, 8}
	mulReply := new(Reply)
	mulCall := client.Go("Arith.Mul", args, mulReply, nil)
	addReply := new(Reply)
	addCall := client.Go("Arith.Add", args, addReply, nil)

	addCall = <-addCall.Done
	if addCall.Error != nil {
		t.Errorf("Add: expected no error but got string %q", addCall.Error.Error())
	}
	if addReply.C != args.A+args.B {
		t.Errorf("Add: expected %d got %d", addReply.C, args.A+args.B)
	}

	mulCall = <-mulCall.Done
	if mulCall.Error != nil {
		t.Errorf("Mul: expected no error but got string %q", mulCall.Error.Error())
	}
	if mulReply.C != args.A*args.B {
		t.Errorf("Mul: expected %d got %d", mulReply.C, args.A*args.B)
	}

	// Error test
	args = &Args{7, 0}
	reply = new(Reply)
	err = client.Call("Arith.Div", args, reply)
	// expect an error: zero divide
	if err == nil {
		t.Error("Div: expected error")
	} else if err.Error() != "divide by zero" {
		t.Error("Div: expected divide by zero error; got", err)
	}

	// Bad type.
	reply = new(Reply)
	err = client.Call("Arith.Add", reply, reply) // args, reply would be the correct thing to use
	if err == nil {
		t.Error("expected error calling Arith.Add with wrong arg type")
	} else if strings.Index(err.Error(), "type") < 0 {
		t.Error("expected error about type; got", err)
	}

	// Non-struct argument
	const Val = 12345
	str := fmt.Sprint(Val)
	reply = new(Reply)
	err = client.Call("Arith.Scan", &str, reply)
	if err != nil {
		t.Errorf("Scan: expected no error but got string %q", err.Error())
	} else if reply.C != Val {
		t.Errorf("Scan: expected %d got %d", Val, reply.C)
	}

	// Non-struct reply
	args = &Args{27, 35}
	str = ""
	err = client.Call("Arith.String", args, &str)
	if err != nil {
		t.Errorf("String: expected no error but got string %q", err.Error())
	}
	expect := fmt.Sprintf("%d+%d=%d", args.A, args.B, args.A+args.B)
	if str != expect {
		t.Errorf("String: expected %s got %s", expect, str)
	}

	args = &Args{7, 8}
	reply = new(Reply)
	err = client.Call("Arith.Mul", args, reply)
	if err != nil {
		t.Errorf("Mul: expected no error but got string %q", err.Error())
	}
	if reply.C != args.A*args.B {
		t.Errorf("Mul: expected %d got %d", reply.C, args.A*args.B)
	}

	// Takes context
	emptyString := ""
	err = client.Call("Arith.TakesContext", "", &emptyString)
	if err != nil {
		t.Errorf("TakesContext: expected no error but got string %q", err.Error())
	}
}

func TestHTTP(t *testing.T) {
	once.Do(startServer)
	testHTTPRPC(t, "")
	newOnce.Do(startNewServer)
	testHTTPRPC(t, newHttpPath)
}

func testHTTPRPC(t *testing.T, path string) {
	var client *Client
	var err error
	if path == "" {
		client, err = DialHTTP("tcp", httpServerAddr)
	} else {
		client, err = DialHTTPPath("tcp", httpServerAddr, path, nil)
	}
	if err != nil {
		t.Fatal("dialing", err)
	}

	// Synchronous calls
	args := &Args{7, 8}
	reply := new(Reply)
	err = client.Call("Arith.Add", args, reply)
	if err != nil {
		t.Errorf("Add: expected no error but got string %q", err.Error())
	}
	if reply.C != args.A+args.B {
		t.Errorf("Add: expected %d got %d", reply.C, args.A+args.B)
	}
}

type ReplyNotPointer int
type ArgNotPublic int
type ReplyNotPublic int
type local struct{}

func (t *ReplyNotPointer) ReplyNotPointer(args *Args, reply Reply) error {
	return nil
}

func (t *ArgNotPublic) ArgNotPublic(args *local, reply *Reply) error {
	return nil
}

func (t *ReplyNotPublic) ReplyNotPublic(args *Args, reply *local) error {
	return nil
}

// Check that registration handles lots of bad methods and a type with no suitable methods.
func TestRegistrationError(t *testing.T) {
	err := Register(new(ReplyNotPointer))
	if err == nil {
		t.Errorf("expected error registering ReplyNotPointer")
	}
	err = Register(new(ArgNotPublic))
	if err == nil {
		t.Errorf("expected error registering ArgNotPublic")
	}
	err = Register(new(ReplyNotPublic))
	if err == nil {
		t.Errorf("expected error registering ReplyNotPublic")
	}
}

type WriteFailCodec int

func (WriteFailCodec) WriteRequest(*Request, interface{}) error {
	// the panic caused by this error used to not unlock a lock.
	return errors.New("fail")
}

func (WriteFailCodec) ReadResponseHeader(*Response) error {
	select {}
}

func (WriteFailCodec) ReadResponseBody(interface{}) error {
	select {}
}

func (WriteFailCodec) Close() error {
	return nil
}

func TestSendDeadlock(t *testing.T) {
	client := NewClientWithCodec(WriteFailCodec(0))

	done := make(chan bool)
	go func() {
		testSendDeadlock(client)
		testSendDeadlock(client)
		done <- true
	}()
	select {
	case <-done:
		return
	case <-time.After(5 * time.Second):
		t.Fatal("deadlock")
	}
}

func testSendDeadlock(client *Client) {
	defer func() {
		recover()
	}()
	args := &Args{7, 8}
	reply := new(Reply)
	client.Call("Arith.Add", args, reply)
}

func dialDirect() (*Client, error) {
	return Dial("tcp", serverAddr)
}

func dialHTTP() (*Client, error) {
	return DialHTTP("tcp", httpServerAddr)
}

func countMallocs(dial func() (*Client, error), t *testing.T) uint64 {
	once.Do(startServer)
	client, err := dial()
	if err != nil {
		t.Fatal("error dialing", err)
	}
	args := &Args{7, 8}
	reply := new(Reply)
	memstats := new(runtime.MemStats)
	runtime.ReadMemStats(memstats)
	mallocs := 0 - memstats.Mallocs
	const count = 100
	for i := 0; i < count; i++ {
		err := client.Call("Arith.Add", args, reply)
		if err != nil {
			t.Errorf("Add: expected no error but got string %q", err.Error())
		}
		if reply.C != args.A+args.B {
			t.Errorf("Add: expected %d got %d", reply.C, args.A+args.B)
		}
	}
	runtime.ReadMemStats(memstats)
	mallocs += memstats.Mallocs
	return mallocs / count
}

func TestCountMallocs(t *testing.T) {
	fmt.Printf("mallocs per rpc round trip: %d\n", countMallocs(dialDirect, t))
}

func TestCountMallocsOverHTTP(t *testing.T) {
	fmt.Printf("mallocs per HTTP rpc round trip: %d\n", countMallocs(dialHTTP, t))
}

type writeCrasher struct {
	done chan bool
}

func (writeCrasher) Close() error {
	return nil
}

func (w *writeCrasher) Read(p []byte) (int, error) {
	<-w.done
	return 0, io.EOF
}

func (writeCrasher) Write(p []byte) (int, error) {
	return 0, errors.New("fake write failure")
}

func TestClientWriteError(t *testing.T) {
	w := &writeCrasher{done: make(chan bool)}
	c := NewClient(w)
	res := false
	err := c.Call("foo", 1, &res)
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() != "fake write failure" {
		t.Error("unexpected value of error:", err)
	}
	w.done <- true
}

func benchmarkEndToEnd(dial func() (*Client, error), b *testing.B) {
	b.StopTimer()
	once.Do(startServer)
	client, err := dial()
	if err != nil {
		b.Fatal("error dialing:", err)
	}

	// Synchronous calls
	args := &Args{7, 8}
	procs := runtime.GOMAXPROCS(-1)
	N := int32(b.N)
	var wg sync.WaitGroup
	wg.Add(procs)
	b.StartTimer()

	for p := 0; p < procs; p++ {
		go func() {
			reply := new(Reply)
			for atomic.AddInt32(&N, -1) >= 0 {
				err := client.Call("Arith.Add", args, reply)
				if err != nil {
					b.Errorf("rpc error: Add: expected no error but got string %q", err.Error())
					return
				}
				if reply.C != args.A+args.B {
					b.Errorf("rpc error: Add: expected %d got %d", reply.C, args.A+args.B)
					return
				}
			}
			wg.Done()
		}()
	}
	wg.Wait()
}

func benchmarkEndToEndAsync(dial func() (*Client, error), b *testing.B) {
	const MaxConcurrentCalls = 100
	b.StopTimer()
	once.Do(startServer)
	client, err := dial()
	if err != nil {
		b.Fatal("error dialing:", err)
	}

	// Asynchronous calls
	args := &Args{7, 8}
	procs := 4 * runtime.GOMAXPROCS(-1)
	send := int32(b.N)
	recv := int32(b.N)
	var wg sync.WaitGroup
	wg.Add(procs)
	gate := make(chan bool, MaxConcurrentCalls)
	res := make(chan *Call, MaxConcurrentCalls)
	b.StartTimer()

	for p := 0; p < procs; p++ {
		go func() {
			for atomic.AddInt32(&send, -1) >= 0 {
				gate <- true
				reply := new(Reply)
				client.Go("Arith.Add", args, reply, res)
			}
		}()
		go func() {
			for call := range res {
				A := call.Args.(*Args).A
				B := call.Args.(*Args).B
				C := call.Reply.(*Reply).C
				if A+B != C {
					b.Errorf("incorrect reply: Add: expected %d got %d", A+B, C)
					return
				}
				<-gate
				if atomic.AddInt32(&recv, -1) == 0 {
					close(res)
				}
			}
			wg.Done()
		}()
	}
	wg.Wait()
}

func BenchmarkEndToEnd(b *testing.B) {
	benchmarkEndToEnd(dialDirect, b)
}

func BenchmarkEndToEndHTTP(b *testing.B) {
	benchmarkEndToEnd(dialHTTP, b)
}

func BenchmarkEndToEndAsync(b *testing.B) {
	benchmarkEndToEndAsync(dialDirect, b)
}

func BenchmarkEndToEndAsyncHTTP(b *testing.B) {
	benchmarkEndToEndAsync(dialHTTP, b)
}
