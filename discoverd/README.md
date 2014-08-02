# Discoverd

[![Build Status](https://travis-ci.org/flynn/discoverd.svg?branch=master)](https://travis-ci.org/flynn/discoverd)

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

Install [Godep](https://github.com/tools/godep).
Clone this repo into `$GOPATH/src/github.com/flynn/discoverd`.
Compile `discoverd`:


```
	$ make build/discoverd
```

To run the tests you'll need `etcd` installed in your PATH.
Follow the [directions](https://github.com/coreos/etcd) for building and installing `etcd`.
Once you have `etcd` installed, you can run the tests:

```
	$ make test
```

## Flynn

[Flynn](https://flynn.io) is a modular, open source Platform as a Service (PaaS). 

If you're new to Flynn, start [here](https://github.com/flynn/flynn).

### Status

Flynn is in active development and **currently unsuitable for production** use. 

Users are encouraged to experiment with Flynn but should assume there are stability, security, and performance weaknesses throughout the project. This warning will be removed when Flynn is ready for production use.

Please report bugs as issues on the appropriate repository. If you have a general question or don't know which repo to use, report them [here](https://github.com/flynn/flynn/issues).

## Contributing

We welcome and encourage community contributions to Flynn.

Since the project is still unstable, there are specific priorities for development. Pull requests that do not address these priorities will not be accepted until Flynn is production ready.

Please familiarize yourself with the [Contribution Guidelines](https://flynn.io/docs/contributing) and [Project Roadmap](https://flynn.io/docs/roadmap) before contributing.

There are many ways to help Flynn besides contributing code:

 - Fix bugs or file issues
 - Improve the [documentation](https://github.com/flynn/flynn.io) including this website
 - [Contribute](https://flynn.io/#sponsor) financially to support core development

Flynn is a [trademark](https://flynn.io/docs/trademark-guidelines) of Prime Directive, Inc.

Now you have a binary you can run. Once you build, you can also run along with etcd for development purposes using Foreman which uses the included Procfile:

```
	$ foreman start
```

This will run both etcd and discoverd. 

## Writing New Backends

A new backing store can be implemented for `discoverd` in Go by implementing the `DiscoveryBackend` interface:

```
type DiscoveryBackend interface {
	Subscribe(name string) (UpdateStream, error)
	Register(name string, addr string, attrs map[string]string) error
	Unregister(name string, addr string) error
}
```

## Writing More Client Libraries

Read the [API docs](https://github.com/flynn/discoverd/blob/master/docs/API.md) to learn how to create a new client library. However, a lot is going on in the [Go client library](https://github.com/flynn/go-discoverd), so that's probably worth reading.
