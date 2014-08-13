---
title: Docs
layout: docs
---

# Flynn Project Docs

## What is Flynn?

Flynn is two things:

1. A "distribution" of components that out-of-the-box gives companies
   a reasonable starting point for an internal "platform" for running their
   applications and services.
2. The banner for a collection of independent projects that together make up
   a toolkit or loose framework for building distributed systems.

Flynn is both a whole and many parts, depending on what is most useful for you.
The common goal is to democratize years of experience and best practices in
building distributed systems. It is the software layer between operators and
developers that makes both their lives easier.

Unlike most PaaS's, Flynn can run stateful services as well as [12
factor](http://12factor.net/) apps. This includes built-in database appliances
(just Postgres to start). Flynn is modular so users can easily modify, upgrade,
and replace components.

Flynn components are divided into two _layers_.

**Layer 0** is a low-level resource framework inspired by the [Google
Omega](http://eurosys2013.tudos.org/wp-content/uploads/2013/paper/Schwarzkopf.pdf)
paper. Layer 0 also includes [service
discovery](https://github.com/flynn/flynn/tree/master/discoverd).

**Layer 1** is a set of higher level components that makes it easy to deploy and
maintain applications and databases.

### Status

Flynn is in active development and **currently unsuitable for production** use.

Users are encouraged to experiment with Flynn but should assume there are
stability, security, and performance weaknesses throughout the project. This
warning will be removed when Flynn is ready for production use.

## Getting Started

We built a [tool](https://dashboard.flynn.io) for launching Flynn clusters on your
Amazon Web Services account [here](https://dashboard.flynn.io).

You can also download a [demo
environment](https://github.com/flynn/flynn/tree/master/demo) for your local
machine or [look at the source on GitHub](https://github.com/flynn/flynn).

## How to use this document

This document is intended to be read primarily for developers and contributors
of the project, however it should provide just as much insight for users of
Flynn. Since Flynn is designed to be understandable and "hackable" by its users,
and as much as possible built with itself, we treat users (both operators and
developers) and contributors as equal citizens.

## Design Philosophy

Before we get started, we want to share our high-level biases and the ideals
that we strive for. Being explicit about this makes it easier for people to
understand the project, which is useful for both understanding how to
contribute, and deciding if Flynn is not right for you. It also holds us
accountable to our own principles.

### Idealized Design

We incorporate Russell Ackoff's concept of [Idealized
Design](http://knowledge.wharton.upenn.edu/article.cfm?articleid=1540) to our
design approach. In short, this means we keep two models of the system in mind.
The first and most important is our ideal. This ever evolving concept of Flynn
is what we could build if we were starting from nothing. By revisiting this
ideal, unconstrained by short-term goals or existing implementations, we can
have an accurate concept of exactly what we want. Without this, progress is
often based on what you want *next*, leading to overly complex systems that
don't accurately reflect the latest understanding of the problem.

From this ideal, we can iteratively converge the existing model of the system
towards a single cohesive goal, as opposed to many different sometimes competing
goals in the form of independent features and fixes.

Knowing what your ideal is not only gives you focus and direction, but it gives
you a chance to continuously apply the latest understanding of the problem and
design principles. The result should be a *simpler* system because you're able
to dissolve problems as opposed to just solving problems.

Some might see this as a real-time version of the second-system syndrome.
However, it's actually about the opposite effect. As Rob Pike
[says](http://commandcenter.blogspot.com/2012/06/less-is-exponentially-more.html),
"The better you understand, the pithier you can be." By continuously moving
towards the ideal based on this understanding, you can achieve a more expressive
system with less.

### Three Worlds Colliding

Flynn is at the intersection of three worlds, each with their own perspective of
how the world should be. When clearly in one world, we try to follow best
practices in that world. However, all three contain great lessons to be applied
in general. The three worlds are: Unix, Web, and Distributed Services.

#### Unix

Unix systems, such as Linux, POSIX, etc, have a very distinct view of the world,
which can be summarized as the [Unix
philosophy](http://en.wikipedia.org/wiki/Unix_philosophy). As you'll see, the
more general parts of the Unix philosophy have had a huge influence on the Flynn
design philosophy.

For the most part, all we want to say here is that when working directly in
a Unix environment, such as inside containers, the Unix philosophy should be
taken to heart and we should be as Unix-like as possible.

This means using as much of the conventional idioms, tools, and infrastructure
(filesystem, pipes, Bash) as we can. Each modern Linux distribution also has
their own idioms and best practices, for example, using Upstart with Ubuntu or
Systemd otherwise.

#### Web

Specifically we're talking about the world of web APIs. To most the "web" means
browsers and HTML, but here we're talking about HTTP and REST APIs. In this
world there is a spectrum of best practices, but there is also an extreme that
we consider but don't force upon ourselves. For this reason, we avoid the loaded
term "RESTful" and use REST-ish, or just say HTTP.

We also believe that HTTP is a sort of [universal
protocol](http://timothyfitz.com/2009/02/12/why-http/). It's well understood,
widely deployed, and very expressive. This is why HTTP is our default choice in
protocol unless there is a specific reason not to use it.

#### Distributed Services

As the knowledge of building complex, scalable, and highly available web
applications has reached more developers, many of the concepts of distributed
systems have been rediscovered. Along with many ideas traditionally found in
"Enterprise Architectures" such as messaging systems and service oriented
architectures. In fact, most modern web architectures are service oriented
architectures.

By distributed services, we mean the body of knowledge behind distributed,
service oriented systems that are behind most large organizations these days,
such as Google and Twitter. While the world of web focuses on the simpler
external web ecosystem, our concept of distributed services focuses on the
complex demands behind an individual service in that ecosystem.

### Simple Components

Given the best practices and ideas from these three worlds, our design
philosophy then centers around one general theme of simple components. Besides
the obvious, this manifests in two desired properties:

1. **Hackability** -- This generally means the system is easy to get into,
   understand, and change. It's not meant to work the single way the designers
   intended, it's meant to be able to work however you want it to, ideally with
   less effort than more.
2. **One Audience** -- As mentioned earlier in this document, we try to make all
   knowledge, documentation, and source behind the project accessible to
   a single audience as opposed to separate user and developer or developer and
   operator audiences. This forces everything to be simple and well documented.

There has been a lot of thought already put into the idea of simplicity in
software. Our favorite works come from the Unix philosophy as mentioned before.
Within this domain, it's easy to find great models for simplicity. For example,
"worse is better", which suffers from a name that is too clever, but is
basically a manifesto for simplicity over perfection.

Another example is the philosophy behind Go, most of which comes from Rob Pike,
who has a long history with Unix and makes arguments like "Less is exponentially
more". However, it's useful for us to get specific, and so we turn to Eric
Raymond's [17 Unix
Rules](https://en.wikipedia.org/wiki/Unix_philosophy#Eric_Raymond.E2.80.99s_17_Unix_Rules).
We're pragmatic idealists, though, so rules are a bit harsh. We consider them
guidelines. Here are six of our favorite, which as you'll see have had a huge
impact on the design of Flynn.

**Rule of Modularity:** Developers should build a program out of simple parts
connected by well defined interfaces, so problems are local, and parts of the
program can be replaced in future versions to support new features. This rule
aims to save time on debugging complex code that is complex, long, and
unreadable.

**Rule of Composition:** Developers should write programs that can communicate
easily with other programs. This rule aims to allow developers to break down
projects into small, simple programs rather than overly complex monolithic
programs.

**Rule of Simplicity:** Developers should design for simplicity by looking for
ways to break up program systems into small, straightforward cooperating pieces.
This rule aims to discourage developers’ affection for writing “intricate and
beautiful complexities” that are in reality bug prone programs.

**Rule of Parsimony:** Developers should avoid writing big programs. This rule
aims to prevent overinvestment of development time in failed or suboptimal
approaches caused by the owners of the program’s reluctance to throw away
visibly large pieces of work. Smaller programs are not only easier to optimize
and maintain; they are easier to delete when deprecated.

**Rule of Diversity:** Developers should design their programs to be flexible
and open. This rule aims to make programs flexible, allowing them to be used in
other ways than their developers intended.

**Rule of Extensibility:** Developers should design for the future by making
their protocols extensible, allowing for easy plugins without modification to
the program's architecture by other developers, noting the version of the
program, and more. This rule aims to extend the lifespan and enhance the utility
of the code the developer writes.

These rules have a common theme, and for our purposes results in the heart of
our design philosophy: simple, composable, extensible components.

## Technical Guidelines

Design philosophy is high level. We also have specific guidelines or policies
that can ease decision-making by providing pre-determined decisions that are
already aligned with our philosophy. Although we are pragmatic about applying
these, we do tend to naturally come back to them anyway. Nevertheless, they are
still only guidelines.

## Project Guidelines

Any healthy open source project has a community around it and those active in
contributing to the project form an ad-hoc organization. Unless it's a standards
body or an Apache project, the organization is often fairly lightweight. But
with time comes culture and with growth comes structure. Both of which are hard
to change. As Pieter Hintjens of ZeroMQ once put it, "Any structure defends
itself."

That said, most of our guidelines around organizing projects (since Flynn is
actually many projects) are about remaining simple and lightweight, but also
include best practices in cultivating cultures that seem to avoid some of the
traps that emerge in some open source projects.

See more at [Contributing](/docs/contributing).
