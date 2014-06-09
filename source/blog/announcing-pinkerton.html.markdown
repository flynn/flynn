---
title: Announcing Pinkerton
date: Jun 8, 2014
---

At Flynn, one of our highest priorities is modularity.

Today we are deeply pleased to announce [Pinkerton](https://github.com/flynn/pinkerton), a tool that allows you to use Docker images with [other container runners](https://github.com/containers/container-rfc#support-matrix).

Users and operators should have complete control over their dependencies and environments. Although many users have experimented with [Docker](http://www.docker.com/)'s initial iteration, but we believe the story of containers, which began over a decade ago with [FreeBSD jails](https://en.wikipedia.org/wiki/FreeBSD_jail) and [Solaris zones](https://en.wikipedia.org/wiki/Solaris_Containers), remains in its infancy. Docker, Inc. introduced many web developers to containers, first through [LXC](http://linuxcontainers.org), and more recently powered by Docker, Inc's own [libcontainer](https://github.com/dotcloud/docker/tree/master/pkg/libcontainer).

Many projects in this space are moving quickly, making great challenges out of compatibility and stability, especially for projects that push the limits of these tools, like Flynn. We have run into more than our share of problems with all of our dependencies, including Docker, which has caused us to redouble our commitment to interoperability and modularity.

Our users should not be tied to Docker, Inc. or any other company.

[Pinkerton](https://github.com/flynn/pinkerton) guarantees container independence permanently. We are also hard at work switching Flynn's container runner to leverage more mature container solutions that guarantee operators the reliability, stability, and performance in production that we demand and users require of all our dependencies.

We have a great deal more to say about container runners, stability, and performance (backed by extensive benchmarks and tests). We are evaluating Red Hat's [libvirt](http://libvirt.org/) and Google's [lmctfy](https://github.com/google/lmctfy), both of which are used by thousands of the industry's most demanding users in production at companies like Google and projects like OpenStack.

Our goal is to provide you with the greatest possible stability in production. Our commitment is unwavering.

Flynn's initial container runner was one of the few components that did not support user-swappability out of the box. Fixing this is currently our highest development priority and a necessity for production stability. [Alternative runners](https://github.com/flynn/flynn-host/tree/libvirt) are already in development on GitHub.

We expect to announce Flynn Beta, suitable for internal services and non-production traffic in the next few weeks. Flynn 1.0 will follow before the end of the summer. Flynn will be the most reliable you use. We look forward to exceeding your expectations.

Because of your support, Flynn will be the single tool that solves ops. Thank you as always for your continued support, enthusiasm, and interest.

--The Flynn Team

p.s. We are in the San Francisco Bay Area for the summer and available to discuss the particular needs of users until the end of August. [Let us know](mailto:contact@flynn.io) if you'd like to meet.
