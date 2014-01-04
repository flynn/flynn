# Discoverd API Reference

Version: 0.1

Discoverd is made to be used with an official library, since a lot of the magic is exposed there. However, the library is just wrapping a client to a fairly simple service API.

## Protocol

Currently the API is exposed using [rpcplus](https://github.com/flynn/rpcplus), a variation of Go's builtin RPC that supports streaming. It supports both Gob and JSON encoding. It will eventually be replaced by [Duplex](https://github.com/progrium/duplex).

All methods return an error value with their output value. If there is no output value, there will still be an error value returned, which will be nil if there was no error.

## Methods

### Agent.Subscribe

Subscribe returns a stream of `ServiceUpdate` objects to replicate the current state of services of a given `Name`. It first immediately sends updates in no particular order of all current services in the set, then as services are added, removed, or changed in the set, updates will be sent. These updates can be used to maintain a local data structure representing a set of services.

#### Input

	type Args struct {
	    Name string
	}

#### Output Stream

	type ServiceUpdate struct {
		Name    string
		Addr    string
		Online  bool
		Created uint
	}

### Agent.Register

Register announces a service of a given `Name` as online at the address `Addr`. `Addr` is formatted as `<ip>:<port>` or just `:<port>`. If only a port is given as the address, discoverd will use the external IP it was configured with. It will return the full value of `Addr` used to register. A service will only remain online if it receives heartbeats at a regular interval to keep it from timing out after 10 seconds.

`Attrs` is an optional string map that lets you specify user-definable attribtues to associate with this service. This can be used for filtering to create subsets of services from `Agent.Subscribe`, or to provide more meta-data about a service.

#### Input

	type Args struct {
		Name 	string
		Addr 	string
		Attrs 	map[string]string
	}

#### Output

	string

### Agent.Unregister

Unregister announces a service of a given `Name` at address `Addr` as offline. `Addr` is formatted as `<ip>:<port>` or just `:<port>`. If only a port is given as the address, discoverd will use the external IP it was configured with.

#### Input

	type Args struct {
		Name string
		Addr string
	}

#### Output

None

### Agent.Heartbeat

Heartbeat will update a service of given `Name` and `Addr` as still online. It must be called regularly or a service will timeout after 10 seconds. `Addr` is formatted as `<ip>:<port>` or just `:<port>`. If only a port is given as the address, discoverd will use the external IP it was configured with.

#### Input

	type Args struct {
		Name string
		Addr string
	}

#### Output

None
