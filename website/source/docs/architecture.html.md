---
title: Architecture
layout: docs
---

## Flynn Architecture

The Flynn architecture is designed to be simple and understandable. Most of the
components of Flynn are no different than the services or applications that are
deployed on top of Flynn. This is because the primary object in the system is
the container and nearly everything runs in a container.

For ease of understanding the significance of the components, Flynn is broken
down into two layers. The first layer contains the minimum components needed to
run the rest of the components -- a bootstrapping layer. The second is where the
rest of Flynn lives.

*Layer 0*, is the core of Flynn. It assumes hosts and a network environment, and
doesn't care how they got there -- cloud or hardware. Layer 0 sits on top of
hosts, abstracting them away and provides primitives for the rest of the system,
namely distributed container management.

*Layer 1* is where most of what we consider Flynn to be exists. It's where
containers become services or applications, and the user workflow is
implemented. Everything in Layer 1 is ultimately no different than the services
it helps to deploy. In fact, we've intentionally stopped at Layer 1 as opposed
to creating a "userland" layer 2 because there would technically be no
difference.

At a high level, here are the concerns of each layer, which map to basic
components. Keep in mind that Layer 1 is based on our MVP requirements, but
extends into anything else Flynn decides to provide.

#### Layer 0

* Distributed configuration / coordination
* Job scheduling
* Service discovery
* Host abstraction

#### Layer 1

* Management API / client
* Git receiver
* Heroku Buildpacks
* Datastore appliances
* Routing

### Layer 0

Layer 0 provides a lower level platform, useful even outside of Flynn, that
provides a solid abstraction between a cluster of hosts and containerized
processes. In that way it's similar to other general scheduling systems, but is
different in these ways:

* It focuses on containers. Everything is run in a high level container, not
  just for security, but for all the reasons containers are powerful.
* It's built from simple parts. Scheduling is broken down into decoupled pieces
  that serve more functions than scheduling. This makes the overall system
  simpler.


#### Distributed configuration / coordination

One of the magic ingredients to any distributed system is a class of distributed
key value stores popularized by Google Chubby and Apache ZooKeeper. These
consistent, consensus algorithm powered stores are swiss army knives for
distributed systems as they can be used for configuration, synchronization, name
resolution, group membership, and more.

Only recently have there been many other options for this class of datastore.
The now defunct Doozer has been spiritually succeeded by CoreOS's
[etcd](https://github.com/coreos/etcd) project and fills the role of
a distributed, consistent, key-value store in Flynn.

While etcd is a great option, all of the Flynn components that rely on it are
designed to allow other backing services. This opens the potential, for example,
for etcd to be replaced by ZooKeeper. In most cases, Flynn components won't work
directly with etcd, but use a specialized service that provides the proper
abstraction and exposes the right semantics for that particular domain. For
example, the routing and service discovery systems.


#### Task scheduling

Scheduling is not found that often in small to medium systems unless something
like Hadoop is used, which often provides its own specialized scheduler.
However, at a certain scale an abstraction becomes necessary to let developers
and operators focus on generic "computation jobs" instead of managing and
configuring hosts and their processes.

Web developers are increasingly familiar with job queues, which can be
considered a very crude form of scheduler, often used for one-off background
tasks. But a scheduler is more general, more intelligent, and provides
a framework to not just run one-off tasks, but services and long-running tasks,
such as the web server or application server itself. Systems with a scheduler at
their core use the scheduler to run everything. It is the interface to the
resources of the cluster.

A scheduling system is often more of a framework to write schedulers. This the
model of Apache Mesos and Google Omega. The framework is responsible for
providing a consistent way to intelligently place tasks based on cluster
resources and whatever other policies are required for that type of task. Hence,
it makes sense to write your own schedulers so you can apply organization
specific policies, such as those required to maintain your SLA.

Flynn investigated two similar but subtly different scheduling systems: Apache
Mesos and Google Omega. While Mesos provided a better understanding of
scheduling as a framework, it seemed that Omega showed that you can achieve
scheduling in a simpler way using existing infrastructure, at least at our
target scale. So for simplicity, we have written our own [scheduling
framework](https://github.com/flynn/flynn/tree/master/host/sampi) that is
loosely based on the concepts of Omega.

Out of the box, Flynn comes with schedulers for different purposes that can
modified or replaced as needed. These schedulers are:

* **Service scheduler** -- Emulates the behavior of Heroku for scheduling
  long-running processes (services).
* **Ephemeral scheduler** -- Simple scheduler for one-off processes, such as
  background jobs or "batch" work, as well as interactive processes (that
  require a TTY).


#### Service discovery

Service discovery is another concept that's often not specifically implemented
in most small to medium systems, usually due to a heavily host-oriented design.
As a result, classic configuration management (Chef, Puppet) is often used.

At it's core, service discovery is just name resolution with realtime presence,
sort of like an IM buddy list. It allows services to find and connect to each
other automatically, and be notified when new instances are available to connect
to and when existing ones go offline.

There aren't a lot of standalone systems for internal service discovery. Most
implementations are intended for WAN or LAN to discover completely vendor
independent services, such as printers. Bonjour is a popular example that uses
various additions to DNS. However, most internal systems use a simpler, more
reliable, centralized approach, such as using ZooKeeper.

Flynn has [implemented service
discovery](https://github.com/flynn/flynn/tree/master/discoverd) as an API
implemented in a library that can be backed by ZooKeeper, mDNS, or in our case
etcd. It lets cooperating services announce themselves and ask for services
they're interested in, getting callbacks when their service list is updated. It
also allows arbitrary key-values to be included on services, allowing for more
advanced querying of services or sharing of meta-data for services.

Non-cooperating services often use service discovery information in
configuration, such as the backends for a load balancer defined in an HAproxy
configuration. A generalized configuration rendering system is used to tie
service discovery into most non-cooperating services.

This thin layer of abstraction on top of etcd provides semantics specific to
service discovery, making it a first-class citizen in the rest of the Flynn
system. It's independent of any RPC or messaging systems, which means it can be
used more generally. For example, you can model hosts as services and use
service discovery to track host availability and any properties that you want to
expose on them, such as available resources.


#### Host abstraction

Flynn mostly abstracts hosts away at Layer 1, but models them in Layer 0.
They're modeled as a service that runs on each host that looks like any other
service in Flynn and speaks the standard RPC language of Flynn.

It acts as an agent representing the host. It exposes information about the host
we're interested in, and lets us perform operations on the host. This mostly
revolves around running containers for the scheduler, but it also provides
resource information used by the scheduler. It also makes itself known using
service discovery.

In a way, the host agent is like the Executor in the Mesos model, letting the
scheduler tell it to run tasks and sharing its state of resources to the
cluster. This is the majority of the responsibility of the agent, but it also
gives us a chance to expose anything else related to host-specific operations.
Effectively turning hosts into container-running appliances.

### Layer 1: Flynn and Beyond

Layer 1 builds on Layer 0 to provide a high-level system that deploys, manages
and scales services on the cluster. This system feels a lot like other platforms
like Heroku, CloudFoundry, OpenShift, etc. but is much more modular and generic.

Layer 1 is built on and hosted by Layer 0, but it doesn't get any special
treatment. It also doesn't depend very heavily on Layer 0, so it would be
possible to replace Layer 0 with another set of services that provide similar
APIs.

Applications deployed using Layer 1 components run on Layer 0 just like
everything else, so there is no "layer 2".

#### Management API / client

The core of Layer 1 is the
[controller](https://github.com/flynn/flynn/tree/master/controller) which
provides a HTTP API and scheduler that model the concept of applications. There
are a few top-level concepts that come together to form a running application.

The first top-level object is an *artifact*. Artifacts are immutable images that
are used to run jobs on Layer 0. Usually this is a URI of a container
image stored somewhere else.

The second top-level object is called a *release*. Releases are immutable
configurations for running one or more processes from an *artifact*. They
contain information about process types including the executable name and
arguments, environment variables, forwarded ports, and resource requirements.

The immutability of artifacts and releases allow for easy rollbacks, and ensure
that no action is destructive.

The third, and last top-level object is the *app*. Apps are essentially
containers that have associated metadata and allow instantiation of running
releases. These running releases are called *formations* and are namespaced
under apps. Formations reference an immutable release and contain a mutable
count of desired running processes for each type.

The controller's scheduler gets updates whenever desired formations change and
then applies changes to the cluster using the Layer 0 scheduling framework.

Every Layer 1 service, including the controller, is an app in the controller and
is scaled and updated using the same controller APIs that are used to manage
every other application. This allows Flynn to be entirely self-bootstrapping and
removes a huge amount of complexity involved in deploying and managing the
platform.

#### Git receiver

The Git receiver is a special-purpose SSH server daemon that receives git
pushes. It turns the pushed revision into a tarball and passes it off the the
Flynn receiver which is responsible for "deploying" the change. There is nothing
special about this component, it just consumes the controller and Layer 0 APIs
and creates a new artifact and release in the controller.

#### Heroku Buildpacks

The receiver uses slugbuilder to run a [Heroku
Buildpack](https://devcenter.heroku.com/articles/buildpacks) against the source
code which turns the code into a runnable "slug". This slug is a tarball of the
app and all of its dependencies which can be run by slugrunner. The final
release in the controller references the slugrunner artifact and the slug via an
environment variable.

#### Routing

The router is responsible for routing and load balancing HTTP and TCP traffic
across backend instances of an app. It uses service discovery to find the
backends, so very little configuration is required. Most incoming public traffic
goes through the router, including that of the controller and Git receiver.
However, the router is not special and it is possible to build your own router
or other service that receives public traffic directly. Most Flynn services
communicate internally directly using service discovery.

The router stores configuration in and synchronizes from etcd (or another
datastore that provides similar guarantees) which allows all instances to have
get the same set of routes right away without restarting. Backends can also come
and go without restarting, since we use streams from service discovery.

#### Datastore appliance framework

The datastore appliance framework allows users not to know how specific
datastores work and provides a standardized management system for stateful
services. The framework is an interface to generically support things like
provisioning databases and credentials, manage backups, automatically
configuring replication, automatic or manual failovers, etc.

Currently Flynn has a PostgreSQL appliance that automatically sets up
replication and does failover, and the appliance framework is in progress.
