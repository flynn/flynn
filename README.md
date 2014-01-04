# Discoverd

A simple but powerful service discovery system written in Go. It's currently backed by etcd, but can be
extended to use ZooKeeper or other distributed consistent stores. 

Right now the only official client is [go-discoverd](https://github.com/flynn/go-discoverd), but it can be ported to any language as it just wraps a simple RPC protocol that talks to the [discoverd API](https://github.com/flynn/discoverd/blob/master/docs/API.md).

## Overview

Discoverd lets your services find each other in a constantly changing environment. With discoverd and a client you can:
 * Register a service as online
 * Locate online instances of a service
 * Get notified when instances of a service change
 * Determine a "leader" for any set of services

There are three pieces to the discoverd system:
 * discoverd itself
 * Client library and API
 * Backend store (etcd, Zookeeper, etc)

The intended configuration is to have your backend store cluster somewhere on your network, the discoverd agent running on all your hosts, and any applications using discoverd to use a client library. 

## Development

First it needs to be compiled:

```
	$ cd discoverd
	$ go build
```

Now you have a binary you can run. Once you build, you can also run along with etcd for development purposes using Foreman which uses the included Procfile:

```
	$ foreman start
```

This will run both etcd and discoverd. 

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

Read the [API docs](https://github.com/flynn/discoverd/blob/master/docs/API.md) to learn how to create a new client library. However, a lot is going on in the [Go client library](https://github.com/flynn/go-discoverd), so that's probably worth reading.

## License

Discover is under the BSD license. See [LICENSE](https://github.com/flynn/go-discover/blob/master/LICENSE) for details.