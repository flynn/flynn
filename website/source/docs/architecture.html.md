---
title: Architecture
layout: docs
---

## Flynn Architecture

We designed the Flynn architecture to be simple and understandable. Most of the components of Flynn are no different than the services or applications you deploy on top of Flynn. This is because the primary object in the system is the container and nearly everything runs in a container.

Flynn's design is composed of two layers. The first layer contains the minimal components needed to run the other services -- a bootstrapping layer. The second layer is where the rest of Flynn lives.

*Layer 0*, is the core of Flynn. It assumes hosts and a network environment, and doesn't care how they got there -- cloud or hardware. Layer 0 sits on top of hosts, abstracting them away and providing primitives for the rest of the system--namely distributed container management.

*Layer 1* is where most of what we consider to be Flynn exists. It's where containers become services or applications and the user workflow is implemented. Everything in Layer 1 is ultimately no different than the services it helps to deploy. In fact, we've intentionally stopped at Layer 1 as opposed to creating a "userland" Layer 2 because there would technically be no difference.

At a high level, here are the concerns of each layer, which map to basic components. Keep in mind that Layer 1 is based on our MVP requirements, but extends into anything else Flynn decides to provide.

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

Layer 0 provides a lower-level platform, useful even outside of Flynn, that provides a solid abstraction between a cluster of hosts and containerized processes. It's like other general scheduling systems, but Flynn differs in these ways:

* It focuses on containers. Flynn runs everything in a high-level container, not only for security, but also for the flexibility containers provide.
* It's built from simple parts. Flynn breaks scheduling down into decoupled pieces that serve more functions than just scheduling. This makes the system simpler.

#### Distributed configuration / coordination

One of the magic ingredients to any distributed system is a class of distributed key value stores popularized by [Google Chubby](http://research.google.com/archive/chubby.html) and [Apache ZooKeeper](https://zookeeper.apache.org). These consistent, consensus-algorithm-powered stores are Swiss Army knives for distributed systems. They can be used for configuration, synchronization, name resolution, group membership, and more.

There weren't many other options for this class of datastore until recently. CoreOS's [etcd](https://github.com/coreos/etcd) project, the spiritual successor to Doozer, fills the role of a distributed, consistent, key-value store in Flynn.

While etcd is a great option, we designed all the Flynn components that rely on it to allow other backing services. This opens up the potential, for example, for [ZooKeeper](https://zookeeper.apache.org) to replace etcd. In most cases, Flynn components don't work directly with etcd. Most use a specialized service that provides the proper abstraction and exposes the right semantics for that particular domain--For example, the routing and service discovery systems.

#### Task scheduling

Scheduling is not often found in small to medium-sized systems unless something like [Hadoop](https://hadoop.apache.org) is used, which often provides its own specialized scheduler. But at a certain scale, an abstraction becomes necessary to let developers and operators focus on generic "computation jobs" instead of managing and configuring hosts and their processes.

Web developers are increasingly familiar with job queues, a crude form of scheduler, often used for one-off background tasks. But a scheduler is more general, more intelligent, and provides a framework for running any kind of task. A scheduler is powerful enough to run services and long-running tasks, such as the web server or application server itself. Systems with a scheduler at their core use the scheduler to run everything. It's the interface to the resources of the cluster.

A scheduling system is often more of a framework to write schedulers. The framework provides a consistent way to divide tasks based on cluster resources and other required policies. It makes sense to write your own schedulers so you can apply organization-specific policies, like those required to maintain your SLA.

Flynn investigated two similar but subtly different scheduling systems: [Apache Mesos](https://mesos.apache.org) and [Google Omega](http://research.google.com/pubs/pub41684.html). While Mesos provided a better understanding of scheduling as a framework, Omega showed that you can achieve scheduling by using existing infrastructure, at least at our target scale. So for simplicity, we have written our own [scheduling framework](https://github.com/flynn/flynn/tree/master/host/sampi) that is loosely based on the concepts of Omega.

Out of the box, Flynn comes with schedulers for different purposes that can be modified or replaced as needed. These schedulers are:

* **Service scheduler** -- Emulates the behavior of Heroku for scheduling long-running processes (services).
* **Ephemeral scheduler** -- Simple scheduler for one-off processes, such as background jobs or "batch" work, as well as interactive processes (that require a TTY).

#### Service discovery

Service discovery is another concept that's often not implemented in most small to medium-sized systems, usually due to a host-oriented design. As a result, classic configuration management (Chef, Puppet) is often used.

At its core, service discovery is just name resolution with realtime presence, sort of like an IM buddy list. Services can find and connect to each other automatically, get notified when new instances are available to connect to, and know when existing instances go offline.

There aren't a lot of standalone systems for internal service discovery. Most implementations are intended for WAN or LAN to discover completely vendor-independent services, such as printers. [Bonjour](https://developer.apple.com/bonjour/index.html) is a popular example that uses various additions to DNS. Most internal systems use a simpler, more reliable, centralized approach, like [ZooKeeper](https://zookeeper.apache.org).

Flynn has [implemented service discovery](https://github.com/flynn/flynn/tree/master/discoverd) as an API implemented in a library that can be backed by ZooKeeper, mDNS, or in our case, etcd. It lets cooperating services announce themselves and ask for services they're interested in, getting callbacks when their service list is updated. It also allows services to include arbitrary key-values, allowing for more advanced querying of services or sharing of meta-data for services.

Non-cooperating services often use service discovery information in configuration, such as the backends for a load balancer defined in an HAproxy configuration. Flynn uses a generalized configuration rendering system to tie service discovery into most non-cooperating services.

This thin layer of abstraction on top of etcd provides semantics specific to service discovery, making it a first-class citizen in the rest of the Flynn system. It's independent of any RPC or messaging systems, which means it can be used more generally. For example, you can model hosts as services and use service discovery to track host availability and any properties that you want to expose on them, such as available resources.


#### Host abstraction

Flynn mostly abstracts hosts away at Layer 1, but models them in Layer 0. They're modeled as a service running on each host that looks like any other service in Flynn and speaks the standard RPC language of Flynn.

Flynn acts as an agent representing the host. It exposes information about the host we're interested in, and lets us perform operations on the host. This revolves around running containers for the scheduler, but it also provides resource information used by the scheduler. It also makes itself known using service discovery.

In a way, the host agent is like the Executor in the Mesos model, letting the scheduler tell it to run tasks and sharing its resources with the cluster. The host agent also gives us a chance to expose anything else related to host-specific operations. This effectively turns hosts into container-running appliances.

### Layer 1: Flynn and Beyond

Layer 1 builds on Layer 0 to provide a high-level system that deploys, manages and scales services on the cluster. This system feels a lot like other platforms such as Heroku, CloudFoundry, OpenShift, etc. but is much more modular and generic.

Layer 1 is built on and hosted by Layer 0, but it doesn't get any special treatment. It also doesn't depend heavily on Layer 0, so it would be possible to replace Layer 0 with another set of services that provide similar APIs.

Applications deployed using Layer 1 components run on Layer 0 just like everything else, so there is no "layer 2".

#### Management API / client

The core of Layer 1 is the [controller](https://github.com/flynn/flynn/tree/master/controller) which provides an HTTP API and scheduler that model the concept of applications. There are a few top-level concepts that come together to form a running application.

The first top-level object is an *artifact*. Artifacts are immutable images that are used to run jobs on Layer 0. Usually this is a URI of a container image stored somewhere else.

The second top-level object is called a *release*. Releases are immutable configurations for running one or more processes from an *artifact*. They contain information about process types including the executable name and arguments, environment variables, forwarded ports, and resource requirements.

Because artifacts and releases are immutable, rollbacks are easy and no action is destructive.

The third, and last top-level object is the *app*. Apps are just containers that have associated metadata and allow instantiation of running releases. These running releases are called *formations* and are namespaced under apps. Formations reference an immutable release and contain a mutable count of desired running processes for each type.

The controller's scheduler gets updates whenever desired formations change and then applies changes to the cluster using the Layer 0 scheduling framework.

Every Layer 1 service, including the controller, is an app in the controller and scales and updates using the same controller APIs that manage every other application. This allows Flynn to be entirely self-bootstrapping and removes a huge amount of complexity involved in deploying and managing the platform.

#### Git receiver

The Git receiver is a special-purpose SSH server daemon that receives git pushes. It turns the pushed revision into a tarball and passes it off the the Flynn receiver which handles "deploying" the change. There is nothing special about this component, it just consumes the controller and Layer 0 APIs and creates a new artifact and release in the controller.

#### Heroku Buildpacks

The receiver uses [slugbuilder](https://github.com/flynn/flynn/tree/master/slugbuilder) to run a [Heroku Buildpack](https://devcenter.heroku.com/articles/buildpacks) against the source code which turns the code into a runnable "slug". This slug is a tarball of the app and all of its dependencies which can be run by [slugrunner](https://github.com/flynn/flynn/tree/master/slugrunner). The final release in the controller references the slugrunner artifact and the slug via an environment variable.

#### Routing

The router handles routing and load balancing HTTP and TCP traffic across backend instances of an app. It uses service discovery to find the backends, so it requires very little configuration. Most incoming public traffic goes through the router, including the controller and Git receiver's traffic. But the router is not special and it's possible to build your own router or other service that receives public traffic directly. Most Flynn services communicate directly using service discovery.

The router stores configuration in and synchronizes with etcd (or another datastore that provides similar guarantees) which allows all instances to get the same set of routes right away without restarting. Backends can also come and go without restarting since we use streams from service discovery.

#### Datastore appliance framework

The datastore appliance framework abstracts away how specific datastores work and provides a standardized management system for stateful services. The framework is an interface to generically support things like provisioning databases and credentials, managing backups, automatically configuring replication, automatic or manual failovers, etc.

Currently Flynn has a PostgreSQL appliance that automatically sets up replication and does failover. The generic appliance framework is in progress.
