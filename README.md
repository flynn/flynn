# Discover for Go (alpha)

A simple but powerful service discovery system written in Go. It's currently backed by etcd, but can be
extended to use ZooKeeper or other distributed consistent stores. The client is lightweight enough to 
also be implemented in other languages.

## Overview

Discover lets your services find each other in a constantly changing environment. With Discover you can:
 * Register a service as online, optionally with user-defined attributes
 * Locate online instances of a service
 * Read attributes of a service or filter services by attributes
 * Get notified when instances of a service change

There are three pieces to the Discover system:
 * Client library and API
 * Discover agent, discoverd
 * Backend store (etcd, Zookeeper, etc)

The intended configuration is to have your backend store cluster somewhere on your network, the Discover agent running on all your hosts, and any applications using Discover to use a client library. The only library is in Go, but it was designed to be available in other languages and eventually will be. 

## Client API Basics

Registering service "cache" already listening on port 9876 with an attribute of `id=cache1`:

```client.Register("cache", 9876, map[string]string{"id": "cache1"})```

This service will now be discoverable until the active heartbeats (done behind the scenes) stop for some reason, or you unregister with:

```client.Unregister("cache", 9876)```

To change attributes, you just re-register. Now to discover other services, say a "queue":

```queues := client.Services("queue")```

Services() returns an object called a `ServiceSet`, which is a dynamic list of available services.

```
for service := range queues.Online() {
	// connect to service.Addr
}
```

At any point you can see which services are online with `.Online()` or check if a service is now offline by looking in `.Offline()`. You can also get notified when services in your `ServiceSet` are updated:

```
updates := make(chan *ServiceUpdates, 10)
queues.Subscribe(updates)
```

Receiving on the `updates` channel will wait until a new update comes about a service in the set with all the properties the service has. It needs to be buffered unless you can ensure it's always receiving, otherwise it will block updates from coming through.

## Advanced API Features

## Starting `discoverd`

## Writing New Backends

A new backing store can be implemented for `discoverd` in Go by implemeting the `DiscoveryBackend` interface:

```
type DiscoveryBackend interface {
	Subscribe(name string) (UpdateStream, error)
	Register(name string, addr string, attrs map[string]string) error
	Unregister(name string, addr string) error
	Heartbeat(name string, addr string) error
}
```

## Writing More Client Libraries

The client library API was designed to be fairly consistent across languages. The client itself is a wrapper for the RPC system that Flynn uses called rpcplus. rpcplus was chosen for it's simplicity, drop-in replacement for Go rpc, and support of streaming responses. This is currently a stopgap RPC solution. Our ideal looks something like Cap'n Proto and msgpack over nanomsg, where it works like RPC but is more stream oriented. The result is a fast, cross-language RPC/messaging hybrid with support for static and dynamic types/languages. 

Until we have this, we may not bother to writing libraries for Discover in other languages, short of the ones necessary for other Flynn components.

## License

Discover is under the BSD license. See [LICENSE](https://github.com/flynn/go-discover/blob/master/LICENSE) for details.