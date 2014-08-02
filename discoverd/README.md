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
