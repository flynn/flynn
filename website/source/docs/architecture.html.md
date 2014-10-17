---
title: Architecture
layout: docs
---

## Flynn Architecture

Flynn's architecture is simple and easy to understand. Most of Flynn's 
system components are no different than the services or applications
deployed on top of Flynn. This is because the primary object in the system is
the "container" and nearly everything runs in a container.

For ease of understanding the significance of the components, Flynn is broken
down into two layers in order to raise the significance of components and emphasize their specific roles. The base layer, "Layer 0" or "Bootstrapping Layer" contains the set of minimum components needed to
start up the system. The main layer, "Layer 1" carries out all of the main tasks.

*Layer 0*, is the core of Flynn and works with an existing hosts and a network infrastructure, either cloud or hardware. Layer 0 is a host abstraction layer providing primitives for the rest of the system,
in particular the distributed container management.

*Layer 1* is Flynn's defining layer.  Layer 1 implements "containers" for services and applications in addition to  "user workflow." 

Today's Layer 1 is based on Flynn's MVP requirements. In an evolving ecosystem there will be additional features developed in the future. Please see the section <Product Roadmap> for additional information.

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

Layer 0 provides a low level platform, useful even outside of Flynn, that
abstracts the activities between a cluster of hosts and containerized
processes. While it is similar to other general scheduling systems, there are main differences:

* Focus on containers. Everything is run in a high level container, not
  just for security, but for all the reasons containers are powerful.
* Built from simple parts. Schedule with decoupled pieces
  that serve more functions than just scheduling.


#### Distributed configuration / coordination

AN important ingredient in any distributed system is a class of distributed
key value stores as popularized by Google Chubby and Apache ZooKeeper. These
stores  can be used for configuration, synchronization, name
resolution, group membership, and more.

Other options for this class of datastore include the CoreOS
[etcd](https://github.com/coreos/etcd) project, sucessor to the Doozer [Heroku reference needed] effort,  filling the role of
a distributed, consistent, key-value store in Flynn.

While etcd is a great option, all of the Flynn components that rely on etcd are
designed to allow other backing services. This opens the potential, for example,
for etcd to be replaced by ZooKeeper. In most cases, Flynn components will not work with etcd without a specialized service that provides the proper
abstraction and exposes the right semantics for that particular domain, for example, the routing and service discovery systems.


#### Task scheduling

Scheduling is not found that often in small to medium systems unless a proprietary specialized scheduler in part of the system.
However, at a certain scale an abstraction becomes necessary to let developers
and operators focus on generic "computation jobs" instead of managing and
configuring hosts and their processes.

Web developers often use job queues, a type of scheduler that is often used for one-off background tasks. A scheduler is more general, more intelligent, and provides a framework to not only run one-off tasks, but also services and long-running tasks, such as the web server or application server. Systems with a scheduler at their core use the scheduler to run everything. It is the interface to the resources of the cluster.

A scheduling system provides a framework for writing schedulers. This the
model used in Apache Mesos and Google Omega. The framework
provides a consistent way to place tasks based on cluster
resources, and set whatever other policies required for the particular type of task. Hence, it makes sense to write schedulers to apply organization
specific policies, such as those required to maintain a Service Level Agreement (SLA).

In order to choose a scheduling system, the Flynn team investigated two scheduling systems: Apache
Mesos and Google Omega. While Mesos provided a better understanding of
scheduling as a framework, Omega's framework showed that you can achieve
provides for a simpler way to use Flynn's existing infrastructure at th target scale. So for simplicity, Flynn's scheduling
framework (https://github.com/flynn/flynn/tree/master/host/sampi) is
loosely based on the concepts of Omega.

Flynn includes multiple schedulers for different purposes that can
modified or replaced as needed. These schedulers are:

* **Service scheduler** -- Emulates the behavior of Heroku for scheduling
  long-running processes (services).
* **Ephemeral scheduler** -- Simple scheduler for one-off processes, such as
  background jobs or "batch" work, as well as interactive processes that
  require a TTY.


#### Service discovery

Service discovery is a feature seldom implemented
in small to medium systems, usually due to a host-oriented design.
As a result, classic configuration management systems such as Chef, or Puppet) are often used.

Service discovery is mainly name resolution with realtime presence,
for example an IM buddy list. It allows services to find and automatically connect with each
other , and accept notififications when new instances are available, and when existing instances go offline.

There are few standalone systems for internal service discovery. Most
implementations for WAN or LAN  discover vendor
independent services, such as printers. Bonjour is a popular example that uses
various additions to DNS. However, most internal systems use a simpler, more
reliable, centralized approach, such as using ZooKeeper.

Flynn's service
discovery (https://github.com/flynn/flynn/tree/master/discoverd) ,
implemented as an API, can be passed in a library backed by ZooKeeper, mDNS, or in Flynn's etcd. Cooperating services that announce themselves and ask for services, get callbacks as the service list updates. It
also allows arbitrary key-values to be included on services, providing for more
advanced querying of services or meta-data sharing of services.

Non-cooperating services often use service discovery information in
configuration, such as the backends for a load balancer defined in an HAproxy
configuration. A generalized configuration rendering system is used to tie
service discovery into most non-cooperating services.

This thin layer of abstraction on top of etcd provides specific service discovery semantics. It's independent of any RPC or messaging systems, assuring its general usage. For example, using model hosts as services,
service discovery tracks host availability and any properties exposed, such as available resources.


#### Host abstraction

Flynn abstracts hosts in Layer 1, and models them in Layer 0.
Modeled as a service, hosts run like any other
service in Flynn, and use Flynn's standard RPC language.

Host abstraction in Flynn uses agents to represent the host. It exposes information about the host, and performs operations on the host. This involves running containers for the scheduler, and it also provides
resource information used by the scheduler and in
service discovery.

In a way, the host agent is similar to the Executor in the Mesos model, where the
scheduler instructs it to run tasks and shares its state of resources with the
cluster. While this is the host agent's main responsibility, but it also
exposes resources related to host-specific operations, 
effectively turning hosts into container-running appliances.

### Layer 1: Flynn and Beyond

Layer 1 builds on Layer 0 to provide a high-level system that deploys, manages
and scales services on the cluster. Similar to other PAAS products such as Heroku, CloudFoundry, OpenShift, Flynn Layer 1 is much more modular and generic.

Layer 1 is built on and hosted by Layer 0, and is treated like any other service. It does not depend very heavily on Layer 0, so it would be
possible to replace Layer 0 with another set of services that provide similar
APIs.

Applications deployed using Layer 1 components run on Layer 0 just like
any other service, so there is no need for a new "layer 2".

#### Management API / client

The core of Layer 1 is the
[controller](https://github.com/flynn/flynn/tree/master/controller) that
provides a HTTP API and scheduler to model applications. There
are a few top-level resources needed to form a running application.

The first top-level object is an *artifact*. Artifacts are immutable images that
are used to run jobs on Layer 0. Usually this is a URI of a persistent container
image.

The second top-level object is called a *release*. Releases are immutable
configurations for running one or more processes from an *artifact*. Releases
contain information about process types including the executable name and
arguments, environment variables, forwarded ports, and resource requirements.

The immutability of artifacts and releases allow for easy rollbacks, and ensure
that no action is destructive.

The third, and last top-level object is the *app*. Apps are
containers including associated metadata that allow instantiation of running
releases. These running releases are called *formations* and are namespaced
under apps. Formations reference an immutable release and contain a mutable
count of desired running processes for each type.

The controller's scheduler updates whenever the desired formations change and
then applies changes to the cluster using the Layer 0 scheduling framework.

Every Layer 1 service, including the controller, is an app in the controller and
scales and updates using the same controller APIs that are used to manage
every other application. Hence Flynn entirely self-bootstrapps and
removes much of the complexity involved in deploying and managing the
platform.

#### Git Receiver

The Git Receiver is a special-purpose SSH server daemon that receives git
pushes. It turns the pushed revision into a tarball then passes it off the the
Flynn receiver responsible for "deploying" the change. This component mainly consumes the controller and Layer 0 APIs,
and creates a new artifact and release in the controller.

#### Heroku Buildpacks

The Flynn Receiver uses slugbuilder to run a [Heroku
Buildpack](https://devcenter.heroku.com/articles/buildpacks) against the source
code that turns the code into a "slug". This slug is a tarball of the
app and all of its dependencies and runs unsing slugrunner. The final
release in the controller references the slugrunner artifact and the slug using a dedicated environment variable.

#### Routing

The Flynn Router is responsible for routing and load balancing HTTP and TCP traffic across backend instances of an app. It uses service discovery to find 
backends, and requires minimum configuration. Most incoming public traffic
goes through the Router, including that of the controller and Git Receiver.
It is possible to build a router or another service that receives public traffic since most Flynn services communicate internally directly using service discovery.

The router stores configuration in etcd and synchronizes from etcd, or another
datastore that provides similar guarantees, which allows all instances to
have the same set of routes without restarting. Backends can be created and destoyed without restarting, since Flynn uses streams from the service discovery.

#### Datastore Appliance Framework

The Datastore Appliance Framework allows users to operate without knowledge about the specific datastore's work and provides a standardized management system for stateful services. The framework is an interface to generically support things like provisioning databases and credentials, manage backups, automatically configuring replication, automatic or manual failovers, etc.

Currently Flynn has a PostgreSQL appliance that automatically sets up
replication and does failover. The Appliance Framework is a work in progress.
