---
title: Architecture
layout: docs
---

## Flynn Architecture

Flynn’s architecture is simple and easy to understand. Most of Flynn’s components are no different than the services or applications that are
deployed on top of Flynn. That’s because the primary object in the system is
the *container* and nearly everything runs in a container.

To help understand the significance of the components, Flynn is broken
down into two layers. Our base layer, **Layer 0**, is a bootstrapping layer. It contains the minimal components needed to
start the rest of the system. **Layer 1** is where the
rest of Flynn lives.

**Layer 0**, is the core of Flynn which communicates with the existing hosts and network infrastructure, both cloud and hardware. Layer 0 abstracts away hosts and provides primitives for Flynn to work with,
making distributed container management simpler.

**Layer 1** is where most of Flynn exists. It's where services and applications are placed into containers and the user workflow is
implemented. Everything in Layer 1 works just like the services
it helps to deploy, which is why there are no additional layers.

Today’s Layer 1 is based on Flynn’s MVP requirements, which you can read more about on our [Product Roadmap](/docs/roadmap), but can expand in the future. Here are the main responsibilities of each layer:

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

Layer 0 provides a low-level platform, useful even outside of Flynn, that
abstracts communications between a cluster of hosts and their containers. While it's similar to other scheduling systems, there are some key differences:

* **Focus on Containers**—Everything is run in a high-level container, not
  only for their security, but also for their flexibility.
* **Simple Parts**—Scheduling is broken down into separate pieces
  that manage more than just scheduling. This makes Flynn easier to understand or improve.


#### Distributed Configuration / Coordination

An important part of any distributed system is a class of 
key value stores popularized by [Google Chubby](http://research.google.com/archive/chubby.html) and [Apache ZooKeeper](https://zookeeper.apache.org). These
consensus-algorithm-powered stores are like Swiss Army knives since they can be used for configuration, synchronization, name
resolution, group membership, and more.

There weren’t many other options for this class of datastore until recently.
CoreOS's
[etcd](https://github.com/coreos/etcd), the spiritual successor to [Doozer](https://github.com/ha/doozerd), fills the role of
a distributed key-value store in Flynn.

While etcd is a great option, we designed all of Flynn’s components to allow other backing services. This opens up the potential, for example,
for [ZooKeeper](https://zookeeper.apache.org) to replace [etcd](https://github.com/coreos/etcd). In most cases, Flynn components don’t work
directly with etcd. They use a specialized service that provides the proper
abstraction for a particular domain—For
example, the routing and service discovery systems.


#### Task Scheduling

Scheduling is not often found in small to medium-sized systems unless something
like [Hadoop](https://hadoop.apache.org) is used, which often provides its own specialized scheduler.
But at a certain scale, abstraction is necessary to let developers
and operators focus on generic "computation jobs" instead of managing hosts and their processes.

Web developers often use job queues, a crude form of scheduler used for one-off background
tasks. A true scheduler is more general, more intelligent, and provides
a framework to run any kind of task. It’s also powerful enough to run services and long-running tasks,
like a web server or the application server itself. Systems with a scheduler at
their core use the scheduler to run everything. It’s the interface to the
resources of the cluster.

A scheduling system provides a framework for writing schedulers. The framework provides a consistent way to divide tasks based on cluster
resources and other required policies. It makes sense to write your own schedulers so you can apply organization-specific policies, like those required to maintain a Service Level Agreement (SLA).

Flynn investigated two similar but subtly different scheduling systems: [Apache Mesos](https://mesos.apache.org) and [Google Omega](http://research.google.com/pubs/pub41684.html). While Mesos provided a better understanding of
scheduling as a framework, Omega showed that you can achieve
scheduling by using existing infrastructure, at least at our
target scale. So for simplicity, we’ve written our own [scheduling
framework](https://github.com/flynn/flynn/tree/master/host/sampi) that is
loosely based on the concepts of Omega.

Flynn includes multiple schedulers for different purposes that can
modified or replaced as needed. These schedulers are:

* **Service scheduler**—Emulates the behavior of Heroku for scheduling
  long-running processes (services).
* **Ephemeral scheduler**—Simple scheduler for one-off processes, such as
  background jobs or "batch" work, as well as interactive processes that require a TTY.


#### Service Discovery

Service discovery is also rarely found
in most small to medium-sized systems, usually due to a host-oriented design.
As a result, configuration management systems like [Chef](https://www.getchef.com/chef/) or [Puppet](https://puppetlabs.com) are often used.

Service discovery is mainly name resolution with realtime presence, like an IM buddy list. Services can find and connect to each other automatically, get notified when new instances are available to connect to, and know when existing instances go offline.

There are few standalone systems for internal service discovery. Most
implementations are intended for WAN or LAN to discover completely vendor-independent services like printers. [Bonjour](https://developer.apple.com/bonjour/index.html) is a popular example that uses
various additions to DNS. Most internal systems use a simpler, more
reliable, centralized approach, like [ZooKeeper](https://zookeeper.apache.org).

Flynn’s [service
discovery](https://github.com/flynn/flynn/tree/master/discoverd), implemented as an API, can be backed by ZooKeeper, mDNS, or, in our case,
etcd. Cooperating services can announce themselves and   get callbacks when their service list is updated. It
also allows services to include arbitrary key-values, allowing more
advanced querying of services or sharing of meta-data for services.

Non-cooperating services often use service discovery information in
configuration, such as the backends for a load balancer defined in an HAproxy
configuration. Flynn uses a generalized configuration rendering system to tie
service discovery into most non-cooperating services.

This thin layer of abstraction on top of etcd provides semantics specific to
service discovery, making it a first-class citizen in the rest of the Flynn
system. It's independent of any RPC or messaging systems, allowing for more general use. For example, you can model hosts as services to track host availability and any exposed properties, such as available resources.


#### Host Abstraction

Flynn abstracts hosts away in Layer 1, but models them in Layer 0.
Hosts look like any other
service in Flynn and use its standard RPC language.

Flynn acts as an agent representing the host. It exposes information about the host and performs operations on it. Operations are mainly running containers for the scheduler, but they also provide
resource information used by the scheduler and service discovery.

The host agent is similar to the Executor in the [Mesos](https://mesos.apache.org) model, letting the
scheduler tell it to run tasks and sharing its resources with the
cluster. It also
gives us a chance to expose anything else related to host-specific operations, effectively turning hosts into container-running appliances.

### Layer 1: Flynn and Beyond

Layer 1 builds on Layer 0 to provide a high-level system that deploys, manages,
and scales services on the cluster. This system feels a lot like other platforms
([Heroku](https://www.heroku.com), [CloudFoundry](http://cloudfoundry.org), [OpenShift](https://www.openshift.com), etc.) but is *much* more modular and generic.

Layer 1 is built on and hosted by Layer 0, but it doesn't get any special
treatment. It also doesn't depend heavily on Layer 0, so it’s
possible to replace Layer 0 with another set of services that provide similar
APIs.

Applications deployed using Layer 1 components run on Layer 0 just like any other service, so there is no "Layer 2".

#### Management API / Client

The core of Layer 1 is the
[controller](https://github.com/flynn/flynn/tree/master/controller) that
provides an HTTP API and scheduler to model applications. There
are a few top-level objects that form a running application.

The first top-level object is an *artifact*. Artifacts are immutable images that
are used to run jobs on Layer 0. Usually this is a URI of a persistent container image.

The second top-level object is called a *release*. Releases are immutable
configurations for running one or more processes from an *artifact*. They
contain information about process types including the executable name and
arguments, environment variables, forwarded ports, and resource requirements. Because artifacts and releases are immutable, rollbacks are easy and no action is destructive.

The third, and final top-level object is the *app*. Apps are essentially
containers and associated metadata that allow instantiation of running
releases. These running releases are called *formations* and are namespaced
under apps. Formations reference an immutable release and contain a mutable
count of desired running processes for each type.

The controller's scheduler gets updates whenever the desired formations change and
then applies changes to the cluster using the Layer 0 scheduling framework.

Every Layer 1 service, including the controller, is an app in the controller that scales and updates using the same controller APIs as
every other application. This allows Flynn to be entirely self-bootstrapping and
removes much of the complexity involved in deploying and managing the
platform.

#### Git Receiver

The Git receiver is a special-purpose SSH server daemon that receives git
pushes. It turns the pushed revision into a tarball and passes it off to the
Flynn receiver responsible for "deploying" the change. It  consumes the controller and Layer 0 APIs
to create a new artifact and release in the controller.

#### Heroku Buildpacks

The Flynn receiver uses slugbuilder to run a [Heroku
Buildpack](https://devcenter.heroku.com/articles/buildpacks) against the source
code that turns the code into a "slug". This slug is a tarball of the
app and any dependencies which can be run by `slugrunner`. The final
release in the controller uses an environment variable to reference the `slugrunner` artifact.

#### Routing

The Flynn router handles routing and load balancing HTTP and TCP traffic
across backend instances of an app. It uses service discovery to find
backends and requires little configuration. Most incoming public traffic
goes through the router, including the controller and Git receiver’s traffic.
It is possible to build your own router
or other service to receive public traffic directly since most Flynn services
communicate directly using service discovery.

The router stores configuration in and synchronizes from etcd (or another
datastore that provides similar guarantees) which allows all instances to 
get the same set of routes right away without restarting. Backends can be created and destroyed without restarting because Flynn uses streams from service discovery.

#### Datastore Appliance Framework

The datastore appliance framework abstracts away how specific
datastores work and provides a standardized management system for stateful
services. The framework is an interface to generically support things like
provisioning databases and credentials, managing backups, automatically
configuring replication, automatically or manually failing over, etc.

Currently Flynn has a PostgreSQL appliance that automatically sets up
replication and does failover. The appliance framework is a **work in progress**.
