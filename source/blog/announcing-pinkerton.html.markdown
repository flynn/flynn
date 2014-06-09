---
title: Announcing Pinkerton
date: Jun 8, 2014
---

At Flynn, one of our highest priorities is modularity.

Today we are deeply pleased to announce [Pinkerton](https://github.com/flynn/pinkerton), a tool that allows you to use Docker images with [other container runners](https://github.com/containers/container-rfc#support-matrix).

We believe that users and operators should have complete control over their dependencies and environments. Docker has been tremendously popular in its initial iteration, but we believe that the story of containers, which began over a decade ago with [FreeBSD jails](https://en.wikipedia.org/wiki/FreeBSD_jail) and [Solaris zones](https://en.wikipedia.org/wiki/Solaris_Containers), remains in its first chapter. Docker introduced many Linux developers to containers, first through [LXC](http://linuxcontainers.org), and more recently powered by Docker, Inc's own [libcontainer](https://github.com/dotcloud/docker/tree/master/pkg/libcontainer).

Most projects in this space are moving quickly, making compatibility and stability great challenges, especially for projects that push their limits, like Flynn. We've run into more than our share of problems with all of our external dependencies, including Docker, which has lead us to redouble our commitment to interoperability and modularity. Flynn users should not be tied to Docker or any other tool. Pinkerton is our first step in an effort to guarantee container independence permanently. We're also hard at work switching Flynn's container backend to leverage a variety of more mature container solutions that can guarantee users and operators the reliability and stability in production that we demand of all our dependencies.

We will have a great deal more to say about container runners and stability (backed by extensive benchmarks and performance tests) and are currently evaluating Red Hat's [libvirt](http://libvirt.org/) and Google's [lmctfy](https://github.com/google/lmctfy), both of which are used extensively in production by thousands of the industry's most demanding users at companies like Google and projects like OpenStack.

Our goal has always been to provide you with the greatest possible stability in production and our commitment is unwavering.

Our containerization backend was one of the few components that did not support user-swappability out of the box. Changing this is currently our highest development priority and an absolute necessity for production stability. [Alternative backends](https://github.com/flynn/flynn-host/tree/libvirt) are already in development on GitHub.

We also expect to announce Flynn Beta, suitable for internal services and non-production traffic in the next few weeks. Flynn 1.0 will follow before the end of the summer. Flynn has to be the most reliable tool operators use and we look forward to living up to your expectations.

Because of your support, Flynn will be the single tool that solves ops for countless developers. Thank you as always for your continued support, enthusiasm, and interest.

--The Flynn Tean

p.s. Our team is in the San Francisco Bay Area for the summer and available to discuss particular needs and use cases until the end of August. [Let us know](mailto:contact@flynn.io) if you'd like to meet.
