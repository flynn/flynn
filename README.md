# Flynn Project Guidebook

## What is Flynn?

Flynn has been marketed as an open source Heroku-like Platform as a Service (PaaS), however the real answer is more subtle.

Flynn is two things:

1) a "distribution" of components that out-of-the-box gives companies a reasonable starting point for an internal "platform" for running their applications and services,

2) the banner for a collection of independent projects that together make up a toolkit or loose framework for building distributed systems.

Flynn is both a whole and many parts, depending on what is most useful for you. The common goal is to democratize years of experience and best practices in building distributed systems. It is the software layer between operators and developers that makes both their lives easier.

## How to use this Guidebook

This guidebook is intended to be read primarily for developers and contributors of the project, however it should provide just as much insight for users of Flynn. Since Flynn is designed to be understandable and "hackable" by its users, and as much as possible built with itself, we treat users (both operators and developers) and contributors as equal citizens.

## Design Philosophy

Before we get started, we want to share our high-level biases and the ideals that we strive for. Being explicit about this makes it easier for people to understand the project, which is useful for both understanding how to contribute, and deciding if Flynn is not right for you. It also holds us accountable to our own principles.

### Idealized Design

We incorporate Russell Ackoff's concept of [Idealized Design](http://knowledge.wharton.upenn.edu/article.cfm?articleid=1540) to our design approach. In short, this means we keep two models of the system in mind. The first and most important is our ideal. This ever evolving concept of Flynn is what we could build if we were starting from nothing. By revisiting this ideal, unconstrained by short-term goals or existing implementations, we can have an accurate concept of exactly what we want. Without this, progress is often based on what you want *next*, leading to overly complex systems that don't accurately reflect the latest understanding of the problem.

From this ideal, we can iteratively converge the existing model of the system towards a single cohesive goal, as opposed to many different sometimes competing goals in the form of independent features and fixes.

Knowing what your ideal is not only gives you focus and direction, but it gives you a chance to continuously apply the latest understanding of the problem and design principles. The result should be a *simpler* system because you're able to dissolve problems as opposed to just solving problems.

Some might see this as a real-time version of the second-system syndrome. However, it's actually about the opposite effect. As Rob Pike [says](http://commandcenter.blogspot.com/2012/06/less-is-exponentially-more.html), "The better you understand, the pithier you can be." By continuously moving towards the ideal based on this understanding, you can achieve a more expressive system with less.

### Three Worlds Colliding

Flynn is at the intersection of three worlds, each with their own perspective of how the world should be. When clearly in one world, we try to follow best practices in that world. However, all three contain great lessons to be applied in general. The three worlds are: Unix, Web, and Distributed Services.

#### Unix

Unix systems, such as Linux, POSIX, etc, have a very distinct view of the world, which can be summarized as the [Unix philosophy](http://en.wikipedia.org/wiki/Unix_philosophy). As you'll see, the more general parts of the Unix philosophy have had a huge influence on the Flynn design philosophy.

For the most part, all we want to say here is that when working directly in a Unix environment, such as inside containers, the Unix philosophy should be taken to heart and we should be as Unix-like as possible.

This means using as much of the conventional idioms, tools, and infrastructure (filesystem, pipes, Bash) as we can. Each modern Linux distribution also has their own idioms and best practices, for example, using Upstart with Ubuntu or Systemd otherwise.

#### Web

Specifically we're talking about the world of web APIs. To most the "web" means browsers and HTML, but here we're talking about HTTP and REST APIs. In this world there is a spectrum of best practices, but there is also an extreme that we consider but don't force upon ourselves. For this reason, we avoid the loaded term "RESTful" and use REST-ish, or just say HTTP.

We also believe that HTTP is a sort of [universal protocol](http://timothyfitz.com/2009/02/12/why-http/). It's well understood, widely deployed, and very expressive. This is why HTTP is our default choice in protocol unless there is a specific reason not to use it.

TODO: little more

#### Distributed Services

As the knowledge of building complex, scalable, and highly available web applications has reached more developers, many of the concepts of distributed systems have been rediscovered. Along with many ideas traditionally found in "Enterprise Architectures" such as messaging systems and service oriented architectures. In fact, most modern web architectures are service oriented architectures.

By distributed services, we mean the body of knowledge behind distributed, service oriented systems that are behind most large organizations these days, such as Google and Twitter. While the world of web focuses on the simpler external web ecosystem, our concept of distributed services focuses on the complex demands behind an individual service in that ecosystem.

TODO: more

### Simple Components

Given the best practices and ideas from these three worlds, our design philosophy then centers around one general theme of simple components. Besides the obvious, this manifests in two desired properties:

1) **Hackability** -- This generally means the system is easy to get into, understand, and change. It's not meant to work the single way the designers intended, it's meant to be able to work however you want it to, ideally with less effort than more.

2) **One Audience** -- As mentioned earlier in this book, we try to make all knowledge, documentation, and source behind the project accessible to a single audience as opposed to separate user and developer or developer and operator audiences. This forces everything to be simple and well documented.

There has been a lot of thought already put into the idea of both simplicity in software. Our favorite works come from the Unix philosophy as mentioned before. Within this domain, it's easy to find great models for simplicity. For example, "worse is better", which suffers from a name that is too clever, but is basically a manifesto for simplicity over perfection.

Another example is the philosophy behind Go, most of which comes from Rob Pike, who has a long history with Unix and makes arguments like "Less is exponentially more". However, it's useful for us to get specific, and so we turn to Eric Raymond's [17 Unix Rules](https://en.wikipedia.org/wiki/Unix_philosophy#Eric_Raymond.E2.80.99s_17_Unix_Rules). We're pragmatic idealists, though, so rules are a bit harsh. We consider them guidelines. Here are six of our favorite, which as you'll see have had a huge impact on the design of Flynn.

**Rule of Modularity:** Developers should build a program out of simple parts connected by well defined interfaces, so problems are local, and parts of the program can be replaced in future versions to support new features. This rule aims to save time on debugging complex code that is complex, long, and unreadable.

**Rule of Composition:** Developers should write programs that can communicate easily with other programs. This rule aims to allow developers to break down projects into small, simple programs rather than overly complex monolithic programs.

**Rule of Simplicity:** Developers should design for simplicity by looking for ways to break up program systems into small, straightforward cooperating pieces. This rule aims to discourage developers’ affection for writing “intricate and beautiful complexities” that are in reality bug prone programs.

**Rule of Parsimony:** Developers should avoid writing big programs. This rule aims to prevent overinvestment of development time in failed or suboptimal approaches caused by the owners of the program’s reluctance to throw away visibly large pieces of work. Smaller programs are not only easier to optimize and maintain; they are easier to delete when deprecated.

**Rule of Diversity:** Developers should design their programs to be flexible and open. This rule aims to make programs flexible, allowing them to be used in other ways than their developers intended.

**Rule of Extensibility:** Developers should design for the future by making their protocols extensible, allowing for easy plugins without modification to the program's architecture by other developers, noting the version of the program, and more. This rule aims to extend the lifespan and enhance the utility of the code the developer writes.

These rules have a common theme, and for our purposes results in the heart of our design philosophy: simple, composable, extensible components.

## Technical Guidelines

Design philosophy is high level. We also have specific guidelines or policies that can ease decision-making by providing pre-determined decisions that are already aligned with our philosophy. Although we are pragmatic about applying these, we do tend to naturally come back to them anyway. Nevertheless, they are still only guidelines.

TODO: the guidelines

## Project Guidelines

Any healthy open source project has a community around it and those active in contributing to the project form an ad-hoc organization. Unless it's a standards body or an Apache project, the organization is often fairly lightweight. But with time comes culture and with growth comes structure. Both of which are hard to change. As Pieter Hintjens of ZeroMQ once put it, "Any structure defends itself."

That said, most of our guidelines around organizing projects (since Flynn is actually many projects) are about remaining simple and lightweight, but also include best practices in cultivating cultures that seem to avoid some of the traps that emerge in some open source projects.

TODO: the guidelines

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

Flynn investigated two similar but subtly different scheduling systems: Apache Mesos and Google Omega. While Mesos provided a better understanding of scheduling as a framework, it seemed that Omega showed that you can achieve scheduling in a simpler way using existing infrastructure, at least at our target scale. So for simplicity, we are writing our own [scheduling framework](https://github.com/flynn/sampi) using the other components in our system that is loosely based on the concepts of Omega.

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
