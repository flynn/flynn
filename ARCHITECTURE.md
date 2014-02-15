## Flynn Architecture

The Flynn architecture is designed to be simple and understandable. Most of the components of Flynn are no different than the services or applications that are deployed on top of Flynn. This is because the primary object in the system is the container and nearly everything runs in a container.

For ease of understanding the significance of the components, Flynn is broken down into two layers. The first layer contains the minimum components needed to run the rest of the components -- a bootstrapping layer. The second is where the rest of Flynn lives.

*Layer 0*, which we call "The Grid", is the core of Flynn. It assumes hosts and network environment, and doesn't care how they got there -- cloud or hardware. The Grid sits on top of hosts, abstracting them away and provides primitives for the rest of the system, namely distributed container management.

*Layer 1* is where most of what we consider Flynn to be exists. It's where containers become services or applications, and the user workflow is implemented. Everything in layer 1 is ultimately no different than the services it helps to deploy. In fact, we've intentionally stopped at layer 1 as opposed to creating a "userland" layer 2 because there would technically be no difference.

At a high level, here are the concerns of each layer, which map to basic components. Keep in mind that Layer 1 is based on our MVP requirements, but extends into anything else Flynn decides to provide.

**Layer 0**
* Container model / management
* Distributed configuration / coordination
* Task scheduling
* Service discovery
* Host abstraction

**Layer 1**
* Management API / client
* Git receiver
* Heroku Buildpacks
* Log aggregation
* Database appliance
* Routing

There are also two concepts that are shared by both and don't "live" anywhere. We'll actually talk about these last.

* Appliance model
* Messaging and RPC model

### Layer 0: The Grid
The Grid provides a lower level platform, useful even outside of Flynn, that  provides a solid abstraction between a cluster of hosts and containerized processes. In that way it's similar to other general scheduling systems, but is different in these ways:

* It focuses on containers. Everything is run in a high level container, not just for security, but for all the reasons containers are powerful.
* It's built from simple parts. Scheduling is broken down into decoupled pieces that serve more functions than scheduling. This makes the overall system simpler.

#### Container model / management
Flynn uses the concept of a high level container being popularized by the project [Docker](https://github.com/dotcloud/docker). In fact, Docker was partly inspired to build projects like Flynn. It provides a standard container model based on the container technology developed by PaaS providers like dotCloud and Heroku.

Docker is the most important and influential of the components in Flynn. Knowing how Flynn works depends fairly heavily on an understanding of Docker and Docker containers. Luckily, Docker is about to become the next big thing, so we'll assume an understanding of Docker from here on.

Docker not only defines what a container is and what you can do with a container, but provides the tools to manage them at a host level. This is what we mean by container management, management at a host level. And since a Docker container is much more than just an LXC container, we also use it as our "model" for what a container is in our system.

#### Distributed configuration / coordination
One of the magic ingredients to any distributed system is a class of distributed key value stores popularized by Google Chubby and Apache ZooKeeper. These consistent, consensus algorithm powered stores are swiss army knives for distributed systems as they can be used for configuration, synchronization, name resolution, group membership, and more.

Only recently have there been many other options for this class of datastore. The now effectively defunct Doozer has been spiritually succeeded by CoreOS's [etcd](https://github.com/coreos/etcd) project. Due to the common outlook between the Flynn team and CoreOS team, a relationship was formed before even knowing about etcd. Now, the etcd project, which is written in Go and speaks HTTP, has found itself as another core component of Flynn.

While etcd is a great option, many of the Flynn components that rely on it are designed to allow other backing services. This opens the potential, for example, for etcd to be replaced by ZooKeeper. In most cases, Flynn components won't work directly with etcd, but use a specialized service that provides the proper abstraction and exposes the right semantics for that particular domain. For example, the scheduling and service discovery systems.

#### Task scheduling
Scheduling is not found that often in small to medium systems unless something like Hadoop is used, which often provides its own specialized scheduler. However, at a certain scale an abstraction becomes necessary to let developers and operators focus on generic "computation jobs" instead of managing and configuring hosts and their processes.

Web developers are increasingly familiar with job queues, which can be considered a very crude form of scheduler, often used for one-off background tasks. But a scheduler is more general, more intelligent, and provides a framework to not just run one-off tasks, but services and long-running tasks, such as the web server or application server itself. Systems with a scheduler at their core use the scheduler to run everything. It is the interface to the resources of the cluster.

A scheduling system is often more of a framework to write schedulers. This the model of Apache Mesos and Google Omega. The framework is responsible for providing a consistent way to intelligently place tasks based on cluster resources and whatever other policies are required for that type of task. Hence, it makes sense to write your own schedulers so you can apply organization specific policies, such as those required to maintain your SLA.

Flynn investigated two similar but subtly different scheduling systems: Apache Mesos and Google Omega. While Mesos provided a better understanding of scheduling as a framework, it seemed that Omega showed that you can achieve scheduling in a simpler way using existing infrastructure, at least at our target scale. So for simplicity, we are writing our own [scheduling framework](https://github.com/flynn/flynn-host/tree/master/sampi) using the other components in our system that is loosely based on the concepts of Omega.

Out of the box, Flynn will come with schedulers for different purposes that can modified or replaced as needed. These schedulers are:

* **Service scheduler** -- Emulates the behavior of Heroku for scheduling long-running processes (services).
* **Ephemeral scheduler** -- Simple scheduler for one-off processes, such as background jobs or "batch" work, as well as interactive processes (that require TTY).
* **Build scheduler** -- Re-implements the Docker Build process as an example of multi-step task orchestration

#### Service discovery
Service discovery is another concept that's often not specifically implemented in most small to medium systems, usually due to a heavily host-oriented design. As a result, classic configuration management (Chef, Puppet) is often used.

At it's core, service discovery is just name resolution with realtime presence, sort of like an IM buddy list. It allows services to find and connect to each other automatically, and be notified when new instances are available to connect to and when existing ones go offline.

There aren't a lot of standalone systems for internal service discovery. Most implementations are intended for WAN or LAN to discover completely vendor independent services, such as printers. Bonjour is a popular example that uses various additions to DNS. However, most internal systems use a simpler, more reliable, centralized approach, such as using ZooKeeper.

Flynn is [implementing service discovery](https://github.com/flynn/go-discover) as an API implemented in a library that can be backed by ZooKeeper, mDNS, or in our case etcd. It lets cooperating services announce themselves and ask for services they're interested in, getting callbacks when their service list is updated. It also allows arbitrary key-values to be included on services, allowing for more advanced querying of services or sharing of meta-data for services.

Non-cooperating services often use service discovery information in configuration, such as the backends for a load balancer defined in an HAproxy configuration. A generalized configuration rendering system is used to tie service discovery into most non-cooperating services.

This thin layer of abstraction on top of etcd provides semantics specific to service discovery, making it a first-class citizen in the rest of the Flynn system. It's independent of any RPC or messaging systems, which means it can be used more generally. For example, you can model hosts as services and use service discovery to track host availability and any properties that you want to expose on them, such as available resources.

#### Host abstraction
Flynn mostly abstracts hosts away at Layer 1, but models them in Layer 0. They're modeled as a service that runs on each host that looks like any other service in Flynn and speaks the standard RPC language of Flynn.

It acts as an agent representing the host. It exposes information about the host we're interested in, and lets us perform operations on the host. This mostly revolves around running containers for the scheduler, but it also provides resource information used by the scheduler. It also makes itself known using service discovery.

In a way, the host agent is like the Executor in the Mesos model, letting the scheduler tell it to run tasks and sharing its state of resources to the cluster. This is the majority of the responsibility of the agent, but it also gives us a chance to expose anything else related to host-specific operations. Effectively turning hosts into container-running appliances.

### Layer 1: Flynn and Beyond

#### Management API / client
#### Git receiver
#### Heroku Buildpacks
#### Log aggregation
#### Database appliance
#### Routing
