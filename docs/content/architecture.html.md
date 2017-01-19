---
title: Architecture
layout: docs
toc_min_level: 2
---

# Architecture

The Flynn architecture is designed to be simple and understandable. Most of the
components of Flynn are no different than the applications that are deployed on
top of Flynn.

Flynn's primary goal is to provide a solid platform to deploy applications on
that is highly available, easy to run and manage, and requires minimal
configuration.

## Building Blocks

Flynn is almost entirely written in Go. Go is one of the few languages designed
with concurrency as a first-class feature which makes it a great fit for Flynn's
distributed systems. Go is statically typed and reasonably fast to compile so we
catch more bugs before they ship. The limited syntax and built-in formatting
tools make contribution and collaboration smoother than any other language we've
used.

JSON over HTTP is used for communication, both internal and external. Our
experience is that it is easy to debug and understand HTTP-based services. We
use [Server-Sent Events](https://www.w3.org/TR/eventsource/) to provide streams
over HTTP. Since most developers are already familiar with HTTP and almost every
language already has great HTTP support, taking advantage of Flynn's APIs is
easier than yet another RPC protocol.

We target the Ubuntu 16.04 LTS amd64 as our base operating system. Flynn has no
hard dependencies on a specific Linux distribution, but experience shows that
the differences between distros are time-consuming to support and irrelevant to
our goals, so we have chosen a single common configuration to support.

## flynn-host

The lowest level component in Flynn is the flynn-host daemon. It is the only
service that doesn't run inside of a container. flynn-host provides an API for
starting and managing containers on a single host.

Flynn runs everything else in containers provided by flynn-host. The container
image and running systems are implementation details. Currently flynn-host
uses `libcontainer` to run containers and a custom system for container images.

The APIs that flynn-host provides are not specific to Linux containers, so we
call a running unit of work a *job*.

## Bootstrapping

After the `flynn-host` daemon is started on some hosts, the bootstrap tool
creates a Flynn cluster on top of them. It reads a JSON manifest that describes
the version and configuration of Flynn to be started and gets everything up and
running by talking to `flynn-host` and the services that are started
subsequently.

After everything is started for the first time, the components work together to
keep the system up and running, even in the face of individual host failures.

## discoverd

One of the most important components of Flynn is discoverd, which provides
service discovery to the cluster. All Flynn components register with discoverd
and then heartbeat every few seconds to confirm that they are still alive.

discoverd provides an API over HTTP that provides access to the list of
registered instances of each service. A compare-and-swap metadata store is also
available for each service, which provides consistent, linearizable storage of
high value configuration values.

The Raft algorithm is used to ensure that data is consistent and stays as available
as possible during failures. A discoverd instance runs on every host in the
cluster, and all instances provide reads. Instances that are not part of the
consensus cluster act as a cache and redirect relevant requests to the leader.

Leader election for each registered service is performed automatically,
selecting the oldest instance of each service as the leader.

Services are also exposed via DNS to allow clients that are not aware of
discoverd's HTTP API to find and communicate with each other.

## flannel

An overlay network is automatically configured using the Linux kernel's native
support for VXLAN encapsulation of Layer 2 frames over UDP. Each host in the
cluster is assigned an /24 block of IPv4 addresses and registered in discoverd.

IPs from the block are allocated to each job that is started by flynn-host.
Routes are configured by flannel that route all inter-container communication to
the correct host.

This combined with discoverd's DNS service discovery allows well-known ports to
be used, avoiding complicated port allocation and NAT.

## PostgreSQL

A state machine persisted in discoverd powers automatic configuration and
failover of a highly available PostgreSQL cluster running within Flynn. We use
synchronous replication in a chained cluster that is easy to reason about to
ensure that no data is lost.

All persistent data stored by Flynn components is in Postgres. It provides
a great combination of features, performance, and reliability that we have not
found in any other database systems.

## Controller

The controller provides a HTTP API that models the high-level concepts that come
together to form a running application in Flynn.

The lowest-level object in the controller is an *artifact*. Artifacts are immutable
images that flynn-host uses to run jobs. Usually this is a URI of a container
image stored somewhere else.

Building on top of artifacts is the *release*. Releases are immutable
configurations for running one or more processes from an artifact. They contain
information about process types including the executable name and arguments,
environment variables, forwarded ports, and resource requirements.

The immutability of artifacts and releases allow for easy rollbacks, and ensure
that no action is destructive.

The final object is the *app*. Apps are namespaces that have associated metadata
and allow instantiation of running releases. These running releases are called
*formations* and are namespaced under apps. Formations reference an immutable
release and contain a mutable count of desired running processes for each
process type.

The controller's scheduler gets updates whenever desired formations change and
ensures that jobs are always running as required by talking to the flynn-host
APIs on each host. A scheduler instance runs on every host in the cluster for
fault tolerance, but only the leader elected by discoverd makes scheduling
decisions.

Every service, including the controller, is an app in the controller and is
scaled and updated using the same APIs that are used to manage every other app.
This allows Flynn to be entirely self-bootstrapping and removes a huge amount of
complexity involved in deploying and managing the platform.

## Router

The router allows external clients to communicate with services running within
Flynn. HTTP requests and TCP connections are load balanced by the router over
backing services based on configured routes.

A route uses discoverd to find the instances of the named service and matches
incoming requests against a pattern: a domain and optional path for HTTP, and
a specific port for TCP.

TLS is also terminated to provide HTTPS with minimal configuration, all that is
required is a TLS certificate chain for the route.

An instance of the router runs on every host to avoid having to think about
where to send client traffic.

## Buildpacks

Flynn uses [Heroku Buildpacks](https://devcenter.heroku.com/articles/buildpacks)
to convert application source code into a runnable artifact called a *slug*.
This slug is a tarball of the app and all of it's dependencies, which can be run
by the *slugrunner* component.

Flynn is designed to support a variety of deployment pipelines, so the buildpack
support is not special or hard-coded, it just uses controller APIs to do the
deploy.

### gitreceive

Git pushes are received over HTTPS by the gitreceive component which spawns
a receiver that starts a new *slugbuilder* job to process the build. If the repo
has been pushed before, a cached archive of the repo will be downloaded from the
*blobstore* before receiving the git push.

### slugbuilder

A slugbuilder job takes an incoming tar stream of the code being deployed,
selects a buildpack and then runs it against the code. The result is a *slug*
that contains the compiled application code and all of its dependencies.

The slug is uploaded to the blobstore, the receiver registers the new artifact
and release in the controller, and then tells the controller to do a rolling
deploy of the new release.

### blobstore

The blobstore provides a simple API for storing and retrieving binary blobs.
Git repositories, app slugs, and buildpack caches are stored in the blobstore.

### slugrunner

Slugrunner is responsible for running a specific process type from an app slug
created by slugbuilder. When an application deployed with the buildpack flow is
scaled up, a slugrunner job is started for each process and then executes the
application from the slug.

## Log Aggregator

The logaggregator takes log lines from jobs running on each host and buffers
recent log lines for each app. The lines are sent from flynn-host as
RFC6587-framed RFC5424 syslog messages over TCP.

Clients can get live streams of aggregated logs, and retrieve previous log
messages without sending requests to every host individually.
