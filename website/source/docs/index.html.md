---
title: Docs
layout: docs
---

# Flynn Project Docs

## What is Flynn?

Flynn is two things:

1. A set of integrated but independent components that gives companies a platform for running and scaling their applications and services, accessible through both a web interface and traditional CLI.
2. The banner for a collection of independent projects that together make up a toolkit or loose framework for building distributed systems.

Flynn is both a complete platform and many independent components, depending on what is most useful for you. The common goal is to assemble years of experience and best practices in building distributed systems. Flynn is the software layer between operators and developers that makes both their lives easier.

Unlike most PaaS's, Flynn can run stateful services as well as [12 factor](http://12factor.net/) apps. This includes built-in database appliances (just Postgres to start). Flynn is modular so users can easily change, upgrade, and replace components.

Flynn is divided into two *layers*.

**Layer 0** is a low-level resource framework inspired by the [Google Omega](http://eurosys2013.tudos.org/wp-content/uploads/2013/paper/Schwarzkopf.pdf) paper. Layer 0 also includes [service discovery](https://github.com/flynn/flynn/tree/master/discoverd).

**Layer 1** is a set of higher-level components that make it easy to deploy and maintain applications and databases.

### Status

Flynn is in active development and **currently unsuitable for production** use.

We encourage you to experiment with Flynn, but you should assume there are stability, security, and performance weaknesses throughout the project. We will remove this warning when Flynn is ready for production use.

## Getting Started

We built a [tool](https://dashboard.flynn.io) for launching Flynn clusters on your Amazon Web Services account [here](https://dashboard.flynn.io).

You can also download a [demo environment](https://github.com/flynn/flynn/tree/master/demo) for your local machine or [look at the source on GitHub](https://github.com/flynn/flynn).

## How to use this document

This document is primarily for developers and contributors to the project, but it should provide just as much information for users of Flynn. Since we designed Flynn to be understandable and "hackable" by anyone, we treat users (both operators and developers) and contributors as equal citizens.

## Design Philosophy

Before we get started, we want to share our high-level biases and the ideals that we strive for. Explaining these goals makes it easier for people to understand the project, understand how to contribute, and decide if Flynn is the right choice. It also holds us accountable to our own principles.

### Three Worlds Colliding

Flynn is at the intersection of three worlds: Unix, the web, and distributed services, each with their own perspective of how things should be. When working completely in one world, we try to follow best practices in that world. But all three contain great lessons we apply across the project.

#### Unix

Unix systems like Linux, POSIX, etc., have a distinct view of the world, which we think of as the [Unix philosophy](http://en.wikipedia.org/wiki/Unix_philosophy). As you'll see, the more general parts of the Unix philosophy have had a huge influence on the Flynn design philosophy.

When working directly in a Unix environment, such as inside containers, the Unix philosophy should be taken to heart and we should be as Unix-like as possible.

This means using as much of the conventional idioms, tools, and infrastructure (file system, pipes, Bash) as we can. Each modern Linux distribution also has their own idioms and best practices; for example, using Upstart with Ubuntu or Systemd otherwise.

#### Web

When we talk about the Web, we're referring to the world of web APIs. To most the "web" means browsers and HTML, but here we're talking about HTTP and REST APIs. In this world there is a spectrum of best practices, but there is also an extreme that we consider but don't force upon ourselves. For this reason, we avoid the loaded term "RESTful" and use REST-ish, or just say HTTP.

We also believe that HTTP is a sort of [universal protocol](http://timothyfitz.com/2009/02/12/why-http/). It's well understood, widely deployed, and very expressive. This is why **HTTP is our default protocol** unless there is a specific reason not to use it.

#### Distributed Services

As more developers start working with complex, scalable and highly-available web applications, they have rediscovered many of the concepts of distributed systems. In fact, most modern web architectures are service-oriented architectures.

By distributed services we mean the body of knowledge behind distributed, service-oriented systems that are created by most large organizations these days, such as Google and Twitter. While the world of the Web focuses on the simpler external Web ecosystem, our concept of distributed services focuses on the complex demands behind an *individual service* in that ecosystem.

### Idealized Design

We incorporate Russell Ackoff's concept of [Idealized Design](http://knowledge.wharton.upenn.edu/article.cfm?articleid=1540) into our approach to the project. In short, this means we keep two models of the system in mind. The first and most important is our ideal. This ever-evolving concept of Flynn is what we could build if we were starting from nothing. By revisiting this ideal, unconstrained by short-term goals or existing implementations, we can have an accurate concept of exactly what we want. Without this, progress is often based on what you want *next*, leading to overly-complex systems that don't accurately reflect the latest understanding of the problem.

With this ideal, we are always moving the system towards a single cohesive goal, as opposed to many different and sometimes competing goals in the form of independent features and fixes. **Our work is performed on the second model, the actual--the product as it exists today.**[FLAG]

Understanding the ideal not only gives you focus and direction, it also gives you a chance to continuously apply the latest understanding of the problem and design principles. By dissolving problems before they appear, rather than solving them after they appear, the system remains simple.

Some might see this as a real-time version of the second-system syndrome, but it actually has the opposite effect. As Rob Pike [says](http://commandcenter.blogspot.com/2012/06/less-is-exponentially-more.html), "The better you understand, the pithier you can be." By continuously moving towards the ideal based on this understanding, you can achieve a more expressive system with less.

### Simple Components

Given the best practices from these three worlds, our design philosophy then centers around simple components. This leads to two key goals in our work:

1. **Hackability** -- This generally means the system is easy to get into, understand, and change. Flynn is not meant to work only in the way the designers intended: it should also be easy to change for your own needs, ideally with as little effort as possible.
2. **One Audience** -- As mentioned earlier in this document, we try to make all knowledge, documentation, and code behind the project accessible to a single audience as opposed to separate *user* and *developer* or *developer* and *operator* audiences. This forces everything to be simple and well documented.

Many engineers have written about the idea of simplicity in software. It's easy to find great models for simplicity in the world of Unix. One of our favorites is [Worse is Better](https://en.wikipedia.org/wiki/Worse_is_better), a manifesto of simplicity over perfection.

We also find a great deal of wisdom in Eric Raymond's [17 Unix Rules](https://en.wikipedia.org/wiki/Unix_philosophy#Eric_Raymond.E2.80.99s_17_Unix_Rules). We're pragmatic idealists, so rules are a bit harsh; We consider them guidelines. Here are six of our favorite which have had a huge impact on the design of Flynn.

####Simple Guidelines

**Rule of Modularity:** Developers should build a program out of simple parts connected by well-defined interfaces so problems are local and parts of the program can be replaced in future versions to support new features. This rule aims to save time on debugging code that is complex, long, and unreadable.

**Rule of Composition:** Developers should write programs that can communicate easily with other programs. This rule aims to allow developers to break down projects into small, simple programs rather than complex, monolithic programs.

**Rule of Simplicity:** Developers should design for simplicity by looking for ways to break up program systems into small, straightforward, cooperating pieces. This rule aims to discourage developers’ affection for writing “intricate and beautiful complexities” that are actually bug-prone programs.

**Rule of Parsimony:** Developers should avoid writing big programs. This rule aims to prevent over-investment of development time in failed or suboptimal approaches caused by the developer’s reluctance to throw away visibly-large pieces of work. Smaller programs are not only easier to optimize and maintain, they are also easier to delete when deprecated.

**Rule of Diversity:** Developers should design their programs to be flexible and open. This rule aims to make programs flexible, allowing them to be used in ways other than what  their developers intended.

**Rule of Extensibility:** Developers should design for the future by making their protocols extensible. They should allow plugins without modifications to the program's architecture, noting the version of the program, or other considerations. This rule aims to extend the lifespan and enhance the utility of the code the developer writes.

These rules have a common theme, and for our purposes results in the heart of our design philosophy: **simple, interchangeable, extensible components**.

## Technical Guidelines (This isn't actually complete)

Design philosophy is high level. We also have specific guidelines or policies that can ease decision-making by providing pre-determined decisions that are already aligned with our philosophy. Although we are pragmatic about applying these, we do tend to naturally come back to them anyway. But they are still only guidelines.

## Project Guidelines

Any healthy open source project has a community around it, and active contributors to the project form an ad-hoc organization. Unless it's a standards body or an Apache project, the organization is often fairly lightweight. But with time comes culture and with growth comes structure. Both of which are hard to change. As Pieter Hintjens of ZeroMQ once put it, "Any structure defends itself."

Most of our organizational guidelines are about remaining simple and lightweight. They also include best practices for forming cultures that avoid the traps that emerge in some open source projects.

See more at [Contributing](/docs/contributing).