---
title: Architecture
layout: docs
---

## Flynn Architecture

We designed Flynn around a set of core beliefs. We believe computers can and
should be reliable and easy to use, even in the most demanding environments. We
believe a single platform should be able to run everything. We believe that
platform should include everything applications, developers, and operators need.
We believe that platform should never go down, and should be incredibly easy to
use.

### One platform

For a single platform to run everything you need, it needs to exceed
expectations. Flynn can run anything that runs on Linux, not just 12 factor
(stateless) web applications. That includes databases, too. It even comes with
PostgreSQL included as an appliance.

### Includes everything

There is a set of basic utilities that applications need to be able to run on
a cluster. Flynn has all of them including service discovery, a router, and log
aggregation. Having all these built in means developers can focus on writing
applications instead of glue code.

### High Availability

Clusters shouldn't go down even if individual machines break. Every component of
Flynn is designed to be highly available. Flynn's scheduler makes sure
applications run across multiple nodes, so the failure of any one won't break
the cluster.

### Developers

Developers using Flynn don't need to do as much work to deploy their
applications. Flynn uses buildpacks to containerize apps and has a web UI as
well as command line interface. Built-in service discovery and databases mean
apps need even less configuration to get up and running.

### Operators

Flynn is fast and easy to install no matter how many machines you use. We even
include an installer for popular cloud providers. Maintenance is a breeze with
zero downtime updates of both your apps and Flynn components. Flynn updates are
frequent and secure so your cluster stays up-to-date.

### Our Tech

We built most of Flynn from scratch, and choose the best tools available.

Flynn is written almost entirely in Go. Go is one of the few languages designed
with concurrency as a first class feature which makes it a great fit for all of
Flynn's distributed systems. Go is statically typed and fast to compile so we
catch more bugs before they ship. Go's limited syntax and built-in formatting
tools make contribution and collaboration smoother than any other language we've
used.

Flynn uses JSON over HTTP for internal communication between services. Since
most developers are already familiar with HTTP and almost every language already
has great HTTP support, taking advantage of Flynn's APIs is easier than yet
another RPC protocol. HTTP is also easy to secure with existing technology,
giving us transport layer security for free.
