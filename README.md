# Welcome to Flynn [![Build Status](https://travis-ci.org/flynn/flynn.svg?branch=master)](https://travis-ci.org/flynn/flynn)

[Flynn](https://flynn.io) is a next generation open source Platform as a Service
(PaaS).

Unlike most PaaS's, Flynn can run stateful services as well as [twelve-factor
](http://12factor.net/) apps. This includes built-in database appliances (just
Postgres to start). Flynn is modular so users can easily modify, upgrade, and
replace components.

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

We have [a web dashboard](https://dashboard.flynn.io) for launching Flynn
clusters on your Amazon Web Services account.

You can also download a [demo environment](/demo) for your local machine or
learn about the components below.

### Trying it out

With a Flynn cluster running and the `flynn` tool installed and configured, the
first thing you'll want to do is add your SSH key so that you can deploy
applications:

```text
flynn key add
```

After adding your ssh key, you can deploy a new application:

```text
git clone https://github.com/flynn/nodejs-flynn-example
cd nodejs-flynn-example
flynn create example
git push flynn master
```

#### Scale

By default there will be one process running, add some more web processes using
the `scale` command:

```text
flynn scale web=3
```

Visit the application [in your browser](http://example.demo.localflynn.com) or with curl:

```text
curl http://example.demo.localflynn.com
```

Repeated requests should show that the requests are load balanced across the
running processes.

#### Logs

`flynn ps` will show the running processes:

```text
$ flynn ps
ID                                             TYPE
e4cffae4ce2b-8cb1212f582f498eaed467fede768d6f  web
e4cffae4ce2b-da9c86b1e9e743f2acd5793b151dcf99  web
e4cffae4ce2b-1b17dd7be8e44ca1a76259a7bca244e1  web
```

To get the log from a process, use `flynn log`:

```text
$ flynn log e4cffae4ce2b-8cb1212f582f498eaed467fede768d6f
Listening on 55007
```

#### Run

An interactive one-off process may be spawned in a container:

```text
flynn run bash
```


### Manual Ubuntu Deployment

Instructions on manually installing and running Flynn can be found
[here](https://flynn.io/docs/installation).


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

Learn more at [flynn.io](https://flynn.io).

Flynn is a [trademark](https://flynn.io/docs/trademark-guidelines) of Prime Directive, Inc.
