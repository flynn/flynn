# Welcome to Flynn [![Build Status](https://travis-ci.org/flynn/flynn.svg?branch=master)](https://travis-ci.org/flynn/flynn)

[Flynn](https://flynn.io) is a next generation open source Platform as a Service
(PaaS).

Unlike most PaaS's, Flynn can run stateful services as well as [12
factor](http://12factor.net/) apps. This includes built-in database appliances
(just Postgres to start). Flynn is modular so users can easily modify, upgrade,
and replace components.

Flynn components are divided into two _layers_.

**Layer 0** is a low-level resource framework inspired by the [Google
Omega](http://eurosys2013.tudos.org/wp-content/uploads/2013/paper/Schwarzkopf.pdf)
paper. Layer 0 also includes [service discovery](/discoverd).

**Layer 1** is a set of higher level components that makes it easy to deploy and
maintain applications and databases.

You can learn more about the project at the [Flynn website](https://flynn.io).

### Status

Flynn is in active development and **currently unsuitable for production** use.

Users are encouraged to experiment with Flynn but should assume there are
stability, security, and performance weaknesses throughout the project. This
warning will be removed when Flynn is ready for production use.

Please **report bugs** as issues on [this
repository](https://github.com/flynn/flynn/issues) after searching to see if
anyone has already reported the issue.

## Getting Started

We built a [tool](https://flynn.cupcake.io) for launching Flynn clusters on your
Amazon Web Services account [here](https://flynn.cupcake.io).

You can also download a [demo environment](/demo) for your local machine or
learn about the components below.

## Components

### Layer 0

**[host](/host)** The Flynn host service, manages containers on each host
and provides the scheduling framework.

**[discoverd](/discoverd)** The Flynn service discovery system.

### Layer 1

**[bootstrap](/bootstrap)** Bootstraps Flynn Layer 1 from a JSON manifest using
the Layer 0 API.

**[controller](/controller)** Provides management and scheduling of applications
running on Flynn via an HTTP API.

**[gitreceived](/gitreceived)** An SSH server made specifically for accepting git pushes.

**[cli](/cli)** Command-line Flynn HTTP API client.

**[receiver](/receiver)** Flynn's git deployer.

**[slugbuilder](/slugbuilder)** Turns a tarball into a Heroku-style "slug" using
[buildpacks](https://devcenter.heroku.com/articles/buildpacks).

**[slugrunner](/slugrunner)** Runs Heroku-like
[slugs](https://devcenter.heroku.com/articles/slug-compiler).

**[router](/router)** Flynn's TCP/HTTP router/load balancer.

**[blobstore](/blobstore)** A simple, fast HTTP file service.

**[sdutil](/sdutil)** Service discovery utility for [discoverd](/discoverd).

**[postgresql](/appliance/postgresql)** Flynn
[PostgreSQL](http://www.postgresql.org/) database appliance.

**[taffy](/taffy)** Taffy pulls git repos and deploys them to Flynn.


## Contributing

We welcome and encourage community contributions to Flynn.

Since the project is still unstable, there are specific priorities for
development. Pull requests that do not address these priorities will not be
accepted until Flynn is production ready.

Please familiarize yourself with the [Contribution
Guidelines](https://flynn.io/docs/contributing) and [Project
Roadmap](https://flynn.io/docs/roadmap) before contributing.

There are many ways to help Flynn besides contributing code:

 - Find bugs and file issues.
 - Improve the [documentation](/website) and website.
 - [Contribute](https://flynn.io/#sponsor) financially to support core development.

Learn more at [flynn.io](https://flynn.io).

Flynn is a [trademark](https://flynn.io/docs/trademark-guidelines) of Prime Directive, Inc.
