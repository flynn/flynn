package main

import (
	"fdrpc"
	"fmt"
	"net"
	"net/rpc"
	"syscall"
)

func main() {
	var dockerInitRpc *rpc.Client
	addr, err := net.ResolveUnixAddr("unix", "/tmp/test.socket")
	if err != nil {
		fmt.Printf("resolv: %v\n", err)
	}
	if socket, err := net.DialUnix("unix", nil, addr); err != nil {
		fmt.Printf("dial Error: %v\n", err)
		return
	} else {
		dockerInitRpc = fdrpc.NewClient(socket)
	}

	var arg int
	var ret fdrpc.RpcFD
	arg = 41
	if err := dockerInitRpc.Call("RpcObject.GetStdOut", &arg, &ret); err != nil {
		fmt.Printf("resume Error: %v\n", err)
		return
	}
	syscall.Write(ret.Fd, []byte("Hello from client 1\n"))

	// Call it again to test multiple calls

	arg = 42
	if err := dockerInitRpc.Call("RpcObject.GetStdOut", &arg, &ret); err != nil {
		fmt.Printf("resume Error: %v\n", err)
		return
	}
	syscall.Write(ret.Fd, []byte("Hello from client 2\n"))
}
